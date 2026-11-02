package hostnav

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/stubbedev/shell-lsp/internal/diskwatch"
	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

const (
	maxLinkedProjects = 8
	stillLinking      = "the rest of the project is still being linked"
	tooLarge          = "the project is too large to follow every file, so not every reader is known"
)

type Stamp struct {
	Docs uint64
	Disk uint64
}

func (s Stamp) before(other Stamp) bool { return s.Docs < other.Docs || s.Disk < other.Disk }

type linkedProject struct {
	partial bool
	linking bool
	key     string
	stamp   Stamp
	dirty   bool
	checked time.Time
	disk    diskwatch.Stamp
	used    uint64
	project *nix.Project
	names   *model.NameTable
	indexes []*model.Index
	files   map[*model.Index]*workspace.File
}

func (l *linkedProject) holds(self *workspace.File) (int, bool) {
	return l.project.FileOf(self.Path, self.Native.Index)
}

type linkJob struct {
	self  *workspace.File
	env   Env
	done  chan struct{}
	since time.Time
}

type Projects struct {
	ctx     context.Context
	stop    context.CancelFunc
	now     func() time.Time
	mu      sync.Mutex
	linked  map[string]*linkedProject
	large   map[string]uint64
	byPath  map[string]*linkedProject
	jobs    map[string]*linkJob
	order   []string
	active  bool
	stamp   Stamp
	cancel  context.CancelFunc
	clock   uint64
	drained func()
}

func NewProjects() *Projects {
	ctx, stop := context.WithCancel(context.Background())
	return &Projects{ctx: ctx, stop: stop, now: time.Now, cancel: func() {}, linked: map[string]*linkedProject{}, large: map[string]uint64{}, byPath: map[string]*linkedProject{}, jobs: map[string]*linkJob{}}
}

func (p *Projects) Close() { p.stop() }

func (p *Projects) Linking() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active
}

func (p *Projects) OnDrained(drained func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.drained = drained
}

func (p *Projects) Want(h Host, env Env) {
	self := h.file
	if self.Path == "" || p.current(self, env) != nil {
		return
	}
	p.enqueue(self, env)
}

func (p *Projects) Forget(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.byPath, path)
	if job, queued := p.jobs[path]; queued {
		close(job.done)
		delete(p.jobs, path)
	}
	referenced := make(map[*linkedProject]bool, len(p.byPath))
	for _, l := range p.byPath {
		referenced[l] = true
	}
	for key, l := range p.linked {
		if !referenced[l] {
			delete(p.linked, key)
		}
	}
}

func (p *Projects) of(ctx context.Context, self *workspace.File, env Env) *linkedProject {
	if l := p.current(self, env); l != nil {
		return l
	}
	job := p.enqueue(self, env)
	if !waitFor(ctx, job.done, env.Patience, p.now().Sub(job.since)) {
		return nil
	}
	return p.current(self, env)
}

func waitFor(ctx context.Context, done <-chan struct{}, patience, spent time.Duration) bool {
	var expired <-chan time.Time
	if patience > 0 {
		timer := time.NewTimer(max(patience-spent, 0))
		defer timer.Stop()
		expired = timer.C
	}
	select {
	case <-done:
		return true
	case <-expired:
	case <-ctx.Done():
	}
	return false
}

func (p *Projects) current(self *workspace.File, env Env) *linkedProject {
	p.mu.Lock()
	l := p.byPath[self.Path]
	if l == nil || l.dirty || l.stamp.before(env.Stamp) {
		p.mu.Unlock()
		return nil
	}
	if _, inside := l.holds(self); !inside {
		p.mu.Unlock()
		return nil
	}
	p.clock++
	l.used = p.clock
	now := p.now()
	due := now.Sub(l.checked) >= workspace.DiskRecheck && !env.Disk.Unchanged(l.disk)
	stamp := env.Disk.Current()
	if due {
		l.checked = now
	}
	p.mu.Unlock()
	if !due {
		return l
	}
	fresh := l.project.Fresh(projectSource{newBuilder(p.ctx, env, host.Nix)})
	p.mu.Lock()
	defer p.mu.Unlock()
	if fresh {
		l.disk = stamp
		return l
	}
	l.dirty = true
	return nil
}

func (p *Projects) enqueue(self *workspace.File, env Env) *linkJob {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.active && p.stamp.before(env.Stamp) {
		p.cancel()
	}
	job, queued := p.jobs[self.Path]
	if queued {
		if job.env.Stamp.before(env.Stamp) {
			job.since = p.now()
		}
		job.self, job.env = self, env
		return job
	}
	job = &linkJob{self: self, env: env, done: make(chan struct{}), since: p.now()}
	p.jobs[self.Path] = job
	p.order = append(p.order, self.Path)
	if !p.active {
		p.active = true
		go p.run()
	}
	return job
}

func (p *Projects) next() (*linkJob, context.Context, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for len(p.order) > 0 {
		path := p.order[0]
		p.order = p.order[1:]
		job, queued := p.jobs[path]
		if !queued {
			continue
		}
		delete(p.jobs, path)
		ctx, cancel := context.WithCancel(p.ctx)
		p.stamp, p.cancel = job.env.Stamp, cancel
		return job, ctx, true
	}
	p.active = false
	return nil, nil, false
}

func (p *Projects) run() {
	for {
		job, ctx, ok := p.next()
		if !ok {
			p.mu.Lock()
			drained := p.drained
			p.mu.Unlock()
			if drained != nil {
				drained()
			}
			return
		}
		l := p.resolve(ctx, job)
		p.mu.Lock()
		if l != nil && ctx.Err() == nil {
			p.byPath[job.self.Path] = l
			p.evict()
		}
		cancel := p.cancel
		p.mu.Unlock()
		cancel()
		close(job.done)
	}
}

func (p *Projects) evict() {
	for len(p.linked) > maxLinkedProjects {
		var oldest *linkedProject
		for _, l := range p.linked {
			if oldest == nil || l.used < oldest.used || l.used == oldest.used && l.key < oldest.key {
				oldest = l
			}
		}
		delete(p.linked, oldest.key)
		for path, l := range p.byPath {
			if l == oldest {
				delete(p.byPath, path)
			}
		}
	}
}

func (p *Projects) resolve(ctx context.Context, job *linkJob) *linkedProject {
	plan, err := planProject(ctx, job.self, job.env)
	if err != nil {
		return nil
	}
	if l := p.planned(ctx, job.env, plan); l != nil {
		return l
	}
	if ctx.Err() != nil {
		return nil
	}
	return p.project(ctx, job.env, plan.alone())
}

func (p *Projects) planned(ctx context.Context, env Env, plan projectPlan) *linkedProject {
	if plan.oversize {
		return nil
	}
	if plan.flake == "" {
		return p.project(ctx, env, plan.rooted())
	}
	base := p.project(ctx, env, plan.flaked(nil))
	if base == nil {
		return nil
	}
	var extra []string
	for _, root := range plan.roots {
		if _, inside := base.project.FileIndex(root); !inside {
			extra = append(extra, root)
		}
	}
	if _, inside := base.project.FileIndex(plan.self.Path); !inside && !slices.Contains(extra, plan.self.Path) {
		extra = append(extra, plan.self.Path)
		slices.Sort(extra)
	}
	if len(extra) == 0 {
		return base
	}
	return p.project(ctx, env, plan.flaked(extra))
}

func (p *Projects) project(ctx context.Context, env Env, request nix.Request) *linkedProject {
	key := projectKey(request)
	p.mu.Lock()
	known := p.linked[key]
	disk, large := p.large[key]
	p.mu.Unlock()
	if large && disk == env.Stamp.Disk {
		return nil
	}
	if known != nil {
		if p.refreshed(known, env) {
			return known
		}
	}
	watched := env.Disk.Current()
	l := linkProject(ctx, env, request)
	if ctx.Err() != nil {
		return nil
	}
	l.key, l.stamp, l.checked, l.disk = key, env.Stamp, p.now(), watched
	p.mu.Lock()
	defer p.mu.Unlock()
	if l.project.Truncated() {
		p.large[key] = env.Stamp.Disk
		delete(p.linked, key)
		return nil
	}
	p.clock++
	l.used = p.clock
	p.linked[key] = l
	for path, held := range p.byPath {
		if held == known && known != nil {
			delete(p.byPath, path)
		}
	}
	return l
}

func (p *Projects) refreshed(l *linkedProject, env Env) bool {
	p.mu.Lock()
	settled := !l.dirty && !l.stamp.before(env.Stamp)
	dirty := l.dirty
	p.mu.Unlock()
	if settled {
		return true
	}
	watched := env.Disk.Current()
	if dirty || !l.project.Fresh(projectSource{newBuilder(p.ctx, env, host.Nix)}) {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	l.stamp, l.checked, l.disk = env.Stamp, p.now(), watched
	return true
}

type projectPlan struct {
	self     *workspace.File
	home     string
	flake    string
	roots    []string
	failure  string
	scanCut  bool
	oversize bool
	shallow  bool
}

func planProject(ctx context.Context, self *workspace.File, env Env) (projectPlan, error) {
	b := newBuilder(ctx, env, host.Nix)
	plan := projectPlan{self: self, home: env.Home, flake: b.nearestFlake(self.Path)}
	var graph *Graph
	if env.Graph != nil {
		found, err := env.Graph(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return projectPlan{}, fmt.Errorf("planning the project of %s: %w", self.Path, ctx.Err())
			}
			plan.failure = "finding the files that import this one: " + err.Error()
		}
		graph = found
		plan.scanCut = graph != nil && graph.cut
	}
	plan.roots = graph.rootsOf(self.Path)
	limit := nix.MaxProjectBytes()
	plan.oversize = graph.reachesPast(limit, plan.flake) || graph.reachesPast(limit, plan.roots...)
	plan.shallow = graph.reachesPast(limit, self.Path)
	return plan, nil
}

func (g *Graph) rootsOf(path string) []string {
	var roots []string
	seen := map[string]bool{path: true}
	queue := []string{path}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		above := 0
		for _, imp := range g.of(at) {
			if imp.module != "" {
				continue
			}
			above++
			if !seen[imp.path] {
				seen[imp.path] = true
				queue = append(queue, imp.path)
			}
		}
		if above == 0 {
			roots = append(roots, at)
		}
	}
	if len(roots) == 0 {
		return []string{path}
	}
	slices.Sort(roots)
	return roots
}

func (plan projectPlan) flaked(extra []string) nix.Request {
	return nix.Request{Home: plan.home, Failure: plan.failure, ScanCut: plan.scanCut, Flake: plan.flake, Roots: extra}
}

func (plan projectPlan) rooted() nix.Request {
	roots := plan.roots
	if !slices.Contains(roots, plan.self.Path) {
		roots = append(slices.Clone(roots), plan.self.Path)
		slices.Sort(roots)
	}
	return nix.Request{Home: plan.home, Failure: plan.failure, ScanCut: plan.scanCut, Roots: roots}
}

func (plan projectPlan) alone() nix.Request {
	return nix.Request{Home: plan.home, Failure: tooLarge, Roots: []string{plan.self.Path}, Shallow: plan.shallow}
}

func projectKey(request nix.Request) string {
	var key strings.Builder
	key.WriteString(request.Flake)
	for _, root := range request.Roots {
		key.WriteByte(0)
		key.WriteString(root)
	}
	key.WriteByte(0)
	key.WriteString(request.Failure)
	if request.ScanCut {
		key.WriteString("\x00cut")
	}
	if request.Shallow {
		key.WriteString("\x00shallow")
	}
	return key.String()
}

func linkProject(ctx context.Context, env Env, request nix.Request, seeds ...*workspace.File) *linkedProject {
	b := newBuilder(ctx, env, host.Nix)
	for _, seed := range seeds {
		b.canonical(seed)
	}
	project := nix.Link(ctx, request, projectSource{b})
	return &linkedProject{project: project, names: &model.NameTable{}, indexes: project.Indexes(), files: b.files}
}

func linkingProject(self *workspace.File) *linkedProject {
	l := unlinkedProject(self, stillLinking)
	l.linking = true
	return l
}

func unlinkedProject(self *workspace.File, reason string) *linkedProject {
	ix := self.Native.Index
	project := nix.Unlinked(ix, self.Path, reason)
	return &linkedProject{partial: true, project: project, names: &model.NameTable{}, indexes: project.Indexes(), files: map[*model.Index]*workspace.File{ix: self}}
}
