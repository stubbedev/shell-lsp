package hostnav

import (
	"cmp"
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/parallel"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

type Loader interface {
	Load(path string) (*workspace.File, error)
	LoadAs(path string, language host.Language) (*workspace.File, error)
	NixFiles(dir string) ([]string, error)
	ScanCut() bool
}

type Env struct {
	Loader Loader
	Home   string
	Graph  func(ctx context.Context) (*Graph, error)
	Exists func(path string) bool

	Background bool
}

type importer struct {
	path   string
	module string
}

type Graph struct {
	importers map[string][]importer
	cut       bool
}

func NewGraph(ctx context.Context, language host.Language, files navigation.WorkspaceFiles, env Env) (*Graph, error) {
	g := &Graph{importers: map[string][]importer{}}
	candidates, err := files(ctx)
	if err != nil {
		return nil, err
	}
	g.cut = env.Loader.ScanCut()
	b := newBuilder(ctx, env, language)
	for _, c := range candidates {
		if c.Native == nil || c.Native.Language != language {
			continue
		}
		c = b.canonical(c)
		for _, r := range b.includes(c) {
			if r.target != nil && !r.include.Probe {
				g.importers[r.target.Path] = append(g.importers[r.target.Path], importer{path: c.Path, module: r.include.Module})
			}
		}
	}
	for path, list := range g.importers {
		slices.SortFunc(list, func(a, b importer) int {
			return cmp.Or(strings.Compare(a.path, b.path), strings.Compare(a.module, b.module))
		})
		g.importers[path] = slices.Compact(list)
	}
	return g, nil
}

func (g *Graph) of(path string) []importer {
	if g == nil {
		return nil
	}
	return g.importers[path]
}

type parent struct {
	module *model.Module
	name   string
}

type Group struct {
	Self     *workspace.File
	Language host.Language
	Module   *model.Module
	self     int
	files    map[*model.Index]*workspace.File
	missing  []model.Include
	includes []resolvedInclude
	parents  []parent
	exists   func(path string) bool
	builder  *builder
}

type builder struct {
	ctx      context.Context
	env      Env
	language host.Language
	files    map[*model.Index]*workspace.File
	byPath   map[string]*workspace.File
	modules  map[string]*model.Module
	resolved map[string][]resolvedInclude
}

type resolvedInclude struct {
	include model.Include
	target  *workspace.File
}

type Host struct {
	file *workspace.File
}

func Of(file *workspace.File) (Host, bool) {
	if file.Native == nil {
		return Host{}, false
	}
	return Host{file: file}, true
}

func (h Host) File() *workspace.File { return h.file }

func (h Host) Language() host.Language { return h.file.Native.Language }

func newBuilder(ctx context.Context, env Env, language host.Language) *builder {
	return &builder{ctx: ctx, env: env, language: language, files: map[*model.Index]*workspace.File{}, byPath: map[string]*workspace.File{}, modules: map[string]*model.Module{}, resolved: map[string][]resolvedInclude{}}
}

func Build(ctx context.Context, h Host, env Env) (*Group, error) {
	if h.Language() == host.Nix {
		return buildProject(ctx, h, env), nil
	}
	var graph *Graph
	if env.Graph != nil {
		var err error
		if graph, err = env.Graph(ctx); err != nil {
			return nil, err
		}
	}
	b := newBuilder(ctx, env, h.Language())
	self := b.canonical(h.file)
	roots := b.roots(self, graph)
	var module *model.Module
	if len(roots) == 1 {
		module = b.module(roots[0])
	} else {
		module = b.merged(roots)
	}
	if slices.ContainsFunc(roots, func(root *workspace.File) bool { return b.fragment(root, graph) }) {
		module.Complete = false
	}
	g := &Group{Self: self, Language: b.language, Module: module, files: b.files, exists: env.Exists}
	g.self = slices.Index(module.Files, self.Native.Index)
	if g.self < 0 {
		panic("hostnav: " + self.URI + " is missing from its own module")
	}
	g.includes = b.includes(self)
	for _, r := range g.includes {
		if r.target == nil && !r.include.Optional && !r.include.Dynamic {
			g.missing = append(g.missing, r.include)
		}
	}
	for _, imp := range graph.of(self.Path) {
		if imp.module == "" {
			continue
		}
		if file := b.load(imp.path); file != nil {
			g.parents = append(g.parents, parent{module: b.module(file), name: imp.module})
		}
	}
	return g, nil
}

func buildProject(ctx context.Context, h Host, env Env) *Group {
	b := newBuilder(ctx, env, h.Language())
	self := b.canonical(h.file)
	module := &model.Module{Complete: true, Builtins: b.language.Builtins(), Submodules: map[string]*model.Module{}, Files: []*model.Index{self.Native.Index}}
	g := &Group{Self: self, Language: b.language, Module: module, files: b.files, exists: env.Exists, builder: b}
	module.Linker = func() model.Links { return g.link() }
	return g
}

func (g *Group) link() model.Links {
	b := g.builder
	request := nix.Request{Self: g.Self.Native.Index, SelfPath: g.Self.Path, Home: b.env.Home}
	var graph *Graph
	if g.Self.Path != "" {
		request.Flake = b.nearestFlake(g.Self.Path)
		if b.env.Graph != nil {
			found, err := b.env.Graph(b.ctx)
			if err != nil {
				request.Failure = "finding the files that import this one: " + err.Error()
			}
			graph = found
			request.ScanCut = graph != nil && graph.cut
		}
		for _, root := range b.roots(g.Self, graph) {
			request.Roots = append(request.Roots, root.Path)
		}
	}
	project := g.cachedProject(request, graph)
	g.Module.Files = project.Indexes()
	return project
}

type projectCell struct {
	mu      sync.Mutex
	project *nix.Project
	graph   *Graph
}

func (g *Group) cachedProject(request nix.Request, graph *Graph) *nix.Project {
	source := projectSource{g.builder}
	cell := workspace.Memo(g.Self, "hostnav.project", 0, func() *projectCell { return &projectCell{} })
	cell.mu.Lock()
	defer cell.mu.Unlock()
	if cell.project != nil && cell.graph == graph && request.Failure == "" && cell.project.Fresh(source) {
		return cell.project
	}
	project := nix.Link(g.builder.ctx, request, source)
	if request.Failure == "" && g.builder.ctx.Err() == nil && !g.builder.env.Background {
		cell.project, cell.graph = project, graph
	}
	return project
}

type Proof struct {
	Doc     *docstore.Document
	Strings []nix.ProvenString
}

func (p Proof) On(doc *docstore.Document) []nix.ProvenString {
	if p.Doc == doc {
		return p.Strings
	}
	return nix.RebasedStrings(p.Strings, p.Doc.Text(), doc.Text())
}

func Prove(ctx context.Context, hosts []Host, env Env) map[string]Proof {
	out := make(map[string]Proof, len(hosts))
	for _, h := range hosts {
		file := h.file
		if _, done := out[file.URI]; done || !nix.MayWrap(file.Native.Index) {
			continue
		}
		if ctx.Err() != nil {
			return nil
		}
		project, linked := buildProject(ctx, h, env).Module.Links().(*nix.Project)
		if !linked {
			panic("hostnav: " + file.URI + " did not link as a Nix project")
		}
		if ctx.Err() != nil {
			return nil
		}
		out[file.URI] = Proof{Doc: file.Doc.Source(), Strings: project.Proven(0)}
		for _, other := range hosts {
			index := other.file.Native.Index
			if i, inside := project.FileOf(other.file.Path, index); inside && nix.MayWrap(index) {
				out[other.file.URI] = Proof{Doc: other.file.Doc.Source(), Strings: project.Proven(i)}
			}
		}
	}
	return out
}

func (b *builder) nearestFlake(path string) string {
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		flake := filepath.Join(dir, nix.FlakeFile)
		if file := b.load(flake); file != nil && file.Native.Nix().IsFlake() {
			return flake
		}
		if dir == filepath.Dir(dir) {
			return ""
		}
	}
}

type projectSource struct{ b *builder }

func (s projectSource) LoadAll(paths []string) []*model.Index {
	loaded := make([]*workspace.File, len(paths))
	parallel.For(s.b.env.workers(), len(paths), func(i int) {
		if s.b.ctx.Err() != nil {
			return
		}
		if file, err := s.b.env.Loader.LoadAs(paths[i], s.b.language); err == nil && file.Native != nil {
			loaded[i] = file
		}
	})
	out := make([]*model.Index, len(paths))
	for i, file := range loaded {
		if file != nil {
			out[i] = s.b.canonical(file).Native.Index
		}
	}
	return out
}

func (s projectSource) Tree(dir string) ([]string, error) {
	files, err := s.b.env.Loader.NixFiles(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return files, err
}

func (s projectSource) Read(path string) ([]byte, error) {
	data, _, err := workspace.ReadDisk(path)
	return data, err
}

func (e Env) workers() int {
	if e.Background {
		return 1
	}
	return runtime.GOMAXPROCS(0)
}

func (b *builder) fragment(root *workspace.File, graph *Graph) bool {
	return !b.language.EntryPoint(root.Path) && len(graph.of(root.Path)) == 0
}

func (b *builder) candidates(path string) []string {
	if strings.HasPrefix(path, "~/") && b.env.Home != "" {
		path = filepath.Join(b.env.Home, path[2:])
	}
	return []string{path}
}

func (b *builder) canonical(f *workspace.File) *workspace.File {
	if known, ok := b.byPath[f.Path]; ok {
		return known
	}
	b.byPath[f.Path] = f
	b.files[f.Native.Index] = f
	return f
}

func (b *builder) includes(f *workspace.File) []resolvedInclude {
	f = b.canonical(f)
	if done, ok := b.resolved[f.Path]; ok {
		return done
	}
	var out []resolvedInclude
	for _, inc := range f.Native.Index.Includes {
		out = append(out, b.resolveInclude(f, inc))
	}
	b.resolved[f.Path] = out
	return out
}

func (b *builder) resolveInclude(f *workspace.File, inc model.Include) resolvedInclude {
	r := resolvedInclude{include: inc}
	if !inc.Dynamic && f.Path != "" {
		r.target = b.find(filepath.Dir(f.Path), inc.Candidates)
	}
	return r
}

func (b *builder) find(dir string, candidates []string) *workspace.File {
	for _, c := range candidates {
		for _, path := range b.candidates(c) {
			if !filepath.IsAbs(path) {
				path = filepath.Join(dir, path)
			}
			if target, err := b.env.Loader.LoadAs(filepath.Clean(path), b.language); err == nil && target.Native != nil {
				return b.canonical(target)
			}
		}
	}
	return nil
}

func (b *builder) closure(root *workspace.File) []*workspace.File {
	root = b.canonical(root)
	out := []*workspace.File{root}
	seen := map[string]bool{root.Path: true}
	for i := 0; i < len(out); i++ {
		for _, r := range b.includes(out[i]) {
			if r.include.Module != "" || r.target == nil || seen[r.target.Path] {
				continue
			}
			seen[r.target.Path] = true
			out = append(out, r.target)
		}
	}
	return out
}

func (b *builder) module(root *workspace.File) *model.Module {
	if m, ok := b.modules[root.Path]; ok {
		return m
	}
	m := &model.Module{Complete: true, Builtins: b.language.Builtins(), Submodules: map[string]*model.Module{}}
	b.modules[root.Path] = m
	b.fill(m, b.closure(root))
	return m
}

func (b *builder) merged(roots []*workspace.File) *model.Module {
	m := &model.Module{Complete: true, Builtins: b.language.Builtins(), Submodules: map[string]*model.Module{}}
	seen := map[string]bool{}
	var files []*workspace.File
	for _, root := range roots {
		for _, f := range b.closure(root) {
			if !seen[f.Path] {
				seen[f.Path] = true
				files = append(files, f)
			}
		}
	}
	b.fill(m, files)
	return m
}

func (b *builder) fill(m *model.Module, files []*workspace.File) {
	for _, f := range files {
		m.Files = append(m.Files, f.Native.Index)
	}
	for _, f := range files {
		for _, r := range b.includes(f) {
			if r.target == nil && (!r.include.Optional || r.include.Dynamic) {
				m.Complete = false
			}
			if r.include.Module == "" {
				continue
			}
			if r.target == nil {
				if _, ok := m.Submodules[r.include.Module]; !ok {
					m.Submodules[r.include.Module] = nil
				}
				continue
			}
			m.Submodules[r.include.Module] = b.module(r.target)
		}
	}
}

func (b *builder) load(path string) *workspace.File {
	file, err := b.env.Loader.LoadAs(path, b.language)
	if err != nil || file.Native == nil {
		return nil
	}
	return b.canonical(file)
}

func (b *builder) roots(self *workspace.File, graph *Graph) []*workspace.File {
	var roots []*workspace.File
	seen := map[string]bool{self.Path: true}
	queue := []*workspace.File{self}
	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]
		above := 0
		for _, imp := range graph.of(f.Path) {
			if imp.module != "" {
				continue
			}
			up := b.load(imp.path)
			if up == nil {
				continue
			}
			above++
			if !seen[up.Path] {
				seen[up.Path] = true
				queue = append(queue, up)
			}
		}
		if above == 0 {
			roots = append(roots, f)
		}
	}
	if len(roots) == 0 {
		return []*workspace.File{self}
	}
	slices.SortFunc(roots, func(a, b *workspace.File) int { return strings.Compare(a.Path, b.Path) })
	return roots
}

func (g *Group) File(ix *model.Index) *workspace.File { return g.files[ix] }

func (g *Group) Index() *model.Index { return g.Module.Files[g.self] }
