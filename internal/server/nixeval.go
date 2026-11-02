package server

import (
	"cmp"
	"context"
	"errors"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/stubbedev/shell-lsp/internal/config"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/host/nix"
	"github.com/stubbedev/shell-lsp/internal/hostnav"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/nixeval"
	"github.com/stubbedev/shell-lsp/internal/regularfile"
)

const (
	nixRequestDeadline = 150 * time.Millisecond
	maxNixpkgsItems    = 500
	maxNixNoticeBytes  = 600
	nixAnyUser         = "<name>"
	nixStringsQuiet    = 750 * time.Millisecond
	nixStringsJob      = "strings"
	nixClassPatience   = 50 * time.Millisecond
	nixMissingNotice   = "shellLsp.nix.evaluate is on, but no nix binary is on PATH: NixOS, home-manager and flake-parts options and nixpkgs attributes are unavailable until it is installed. Static Nix features keep working."
)

var nixUserOptions = []string{"home-manager", "users", nixAnyUser}

type nixEvaluator interface {
	Warm(dir string) *nixeval.Refresh
	OptionSources(ctx context.Context, dir string) (nixeval.Answer[[]nixeval.OptionSource], error)
	Options(ctx context.Context, dir string, source nixeval.OptionSource) (nixeval.Answer[*nixeval.OptionTree], error)
	BaseOptions(ctx context.Context, dir string) (nixeval.Answer[*nixeval.OptionTree], error)
	NixpkgsAttrs(ctx context.Context, dir string, parent []string, prefix string) (nixeval.Answer[[]string], error)
	NixpkgsPackage(ctx context.Context, dir string, path []string) (nixeval.Answer[*nixeval.Package], error)
	Close()
}

func openNixClient(report func(error)) (nixEvaluator, error) {
	client, err := nixeval.New(report)
	if err != nil {
		return nil, err
	}
	return client, nil
}

type nixEvaluation struct {
	open func(report func(error)) (nixEvaluator, error)

	mu      sync.Mutex
	armed   bool
	stopped bool
	failed  bool
	session *nixSession

	classPatience time.Duration

	strings nixStrings
}

type nixStrings struct {
	quiet   time.Duration
	proving sync.Mutex

	mu        sync.Mutex
	stopped   bool
	scheduler *pushScheduler
	proofs    map[string]hostnav.Proof
	revision  uint64
}

func (n *nixStrings) of(uri string) (hostnav.Proof, uint64, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	proof, held := n.proofs[uri]
	return proof, n.revision, held
}

func (n *nixStrings) forget(uri string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.proofs, uri)
}

func (n *nixStrings) publish(proofs map[string]hostnav.Proof, open func(uri string) bool) bool {
	next := make(map[string]hostnav.Proof, len(proofs))
	for uri, proof := range proofs {
		if len(proof.Strings) > 0 && open(uri) {
			next[uri] = proof
		}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	changed := len(next) != len(n.proofs)
	for uri, proof := range next {
		held, had := n.proofs[uri]
		changed = changed || !had || !slices.Equal(held.On(proof.Doc), proof.Strings)
	}
	n.proofs = next
	if changed {
		n.revision++
	}
	return changed
}

func (n *nixStrings) stop() {
	n.mu.Lock()
	n.stopped = true
	scheduler := n.scheduler
	n.mu.Unlock()
	if scheduler != nil {
		scheduler.close()
	}
}

func (s *Server) nixDocumentsChanged(uris []string) {
	view := s.docs.View()
	if slices.ContainsFunc(uris, func(uri string) bool {
		doc, open := view.Document(uri)
		if !open {
			return false
		}
		language, known := host.Of(doc.URI, doc.Language)
		return known && language == host.Nix
	}) {
		s.scheduleNixStrings()
	}
}

func (s *Server) scheduleNixStrings() {
	n := &s.nixEval.strings
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.stopped {
		return
	}
	if n.scheduler == nil {
		n.scheduler = newPushScheduler(cmp.Or(n.quiet, nixStringsQuiet), s.proveNixStrings)
	}
	n.scheduler.schedule(nixStringsJob)
}

func (s *Server) proveNixStrings(ctx context.Context, _ string) {
	n := &s.nixEval.strings
	n.proving.Lock()
	defer n.proving.Unlock()
	docs := s.docs.View()
	var hosts []hostnav.Host
	for _, doc := range docs.Documents() {
		if language, known := host.Of(doc.URI, doc.Language); !known || language != host.Nix {
			continue
		}
		snap, err := s.snapshot(docs, doc.URI)
		if err != nil {
			s.logger.Debug("proving the shell strings of a closed document", "uri", doc.URI)
			continue
		}
		if h, ok := hostnav.Of(snap.File); ok {
			hosts = append(hosts, h)
		}
	}
	slices.SortFunc(hosts, func(a, b hostnav.Host) int { return strings.Compare(a.File().URI, b.File().URI) })
	env := s.hostEnv(docs, host.Nix)
	env.Background = true
	proofs := hostnav.Prove(ctx, hosts, env)
	if ctx.Err() != nil || !n.publish(proofs, s.docs.Has) {
		return
	}
	s.redeliverNixStrings()
}

func (s *Server) redeliverNixStrings() {
	ctx := context.Background()
	view := s.docs.View().Documents()
	uris := make([]string, len(view))
	for i, doc := range view {
		uris[i] = doc.URI
	}
	s.delivery.redeliver(ctx, uris)
	if s.clientCapabilities().Workspace.SemanticTokens.RefreshSupport {
		s.requestRefresh(ctx, semanticTokensRefreshMethod)
	}
}

func (e *nixEvaluation) arm() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.armed = true
}

type nixSession struct {
	client     nixEvaluator
	closing    chan struct{}
	watchers   sync.WaitGroup
	classLinks sync.Mutex

	mu      sync.Mutex
	closed  bool
	warmed  map[string]bool
	warming map[string]bool
	watched map[<-chan struct{}]bool
	classes map[string]*nixClassLookup
}

type nixClassLookup struct {
	stamp   string
	done    chan struct{}
	cancel  context.CancelFunc
	found   bool
	classes []nix.OptionClass
	earlier []nix.OptionClass
}

func (n *nixSession) close() {
	n.mu.Lock()
	n.closed = true
	for _, lookup := range n.classes {
		lookup.cancel()
	}
	n.mu.Unlock()
	close(n.closing)
	n.client.Close()
	n.watchers.Wait()
}

func (n *nixSession) classLookup(uri, stamp string, find func(context.Context) []nix.OptionClass) *nixClassLookup {
	n.mu.Lock()
	defer n.mu.Unlock()
	previous, held := n.classes[uri]
	if held && previous.stamp == stamp {
		return previous
	}
	ctx, cancel := context.WithCancel(context.Background())
	lookup := &nixClassLookup{stamp: stamp, done: make(chan struct{}), cancel: cancel}
	if held {
		previous.cancel()
		lookup.earlier = previous.settled()
	}
	if n.closed {
		cancel()
		close(lookup.done)
		return lookup
	}
	n.classes[uri] = lookup
	n.watchers.Go(func() {
		defer cancel()
		defer close(lookup.done)
		n.classLinks.Lock()
		defer n.classLinks.Unlock()
		if ctx.Err() != nil {
			return
		}
		classes := find(ctx)
		if ctx.Err() == nil {
			lookup.classes, lookup.found = classes, true
		}
	})
	return lookup
}

func (l *nixClassLookup) settled() []nix.OptionClass {
	select {
	case <-l.done:
		if l.found {
			return l.classes
		}
	default:
	}
	return l.earlier
}

func (n *nixSession) forgetClasses(uri string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if lookup, held := n.classes[uri]; held {
		lookup.cancel()
		delete(n.classes, uri)
	}
}

func (n *nixSession) watch(refresh *nixeval.Refresh, finished func(error)) {
	done := refresh.Done()
	select {
	case <-done:
		finished(refresh.Err())
		return
	default:
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed || n.watched[done] {
		return
	}
	n.watched[done] = true
	n.watchers.Go(func() {
		select {
		case <-done:
			finished(refresh.Err())
		case <-n.closing:
		}
		n.mu.Lock()
		defer n.mu.Unlock()
		delete(n.watched, done)
	})
}

func (n *nixSession) claimWarm(dir string, force bool) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed || n.warming[dir] || (!force && n.warmed[dir]) {
		return false
	}
	n.warming[dir], n.warmed[dir] = true, true
	return true
}

func (n *nixSession) warmFinished(dir string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.warming, dir)
}

func (s *Server) nixSession() *nixSession {
	session, err := s.openNixSession()
	if err != nil {
		s.nixProblem(err)
	}
	return session
}

func (s *Server) openNixSession() (*nixSession, error) {
	e := &s.nixEval
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.armed || e.stopped || e.failed {
		return e.session, nil
	}
	if e.session != nil {
		return e.session, nil
	}
	open := e.open
	if open == nil {
		open = openNixClient
	}
	client, err := open(func(err error) { s.logger.Warn("nix evaluation", "err", err) })
	if err != nil {
		e.failed = true
		return nil, err
	}
	e.session = &nixSession{client: client, closing: make(chan struct{}), warmed: map[string]bool{}, warming: map[string]bool{}, watched: map[<-chan struct{}]bool{}, classes: map[string]*nixClassLookup{}}
	return e.session, nil
}

func (s *Server) closeNixSession(stop bool) {
	e := &s.nixEval
	e.mu.Lock()
	session := e.session
	e.session, e.failed = nil, false
	e.stopped = e.stopped || stop
	e.mu.Unlock()
	if session != nil {
		session.close()
	}
}

func (s *Server) forgetNixClasses(uri string) {
	e := &s.nixEval
	e.mu.Lock()
	session := e.session
	e.mu.Unlock()
	if session != nil {
		session.forgetClasses(uri)
	}
}

func (s *Server) stopNixEvaluation() {
	s.closeNixSession(true)
	s.nixEval.strings.stop()
}

func (s *Server) nixEvaluationWanted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings.Nix.Evaluate || slices.ContainsFunc(slices.Collect(maps.Values(s.folderSettings)), func(settings config.Settings) bool { return settings.Nix.Evaluate })
}

func (s *Server) syncNixEvaluation() {
	if !s.nixEvaluationWanted() {
		s.closeNixSession(false)
		return
	}
	for _, root := range s.workspace.Roots() {
		flake := filepath.Join(root, nix.FlakeFile)
		if _, err := regularfile.Stat(flake); err == nil {
			s.warmFlakeOf(docstore.Location{Path: flake, Local: true}, false)
		}
	}
}

func (s *Server) nixFlakeDir(path string) (string, bool) {
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		if s.isFlakeDir(dir) {
			return dir, true
		}
		if dir == filepath.Dir(dir) {
			return "", false
		}
	}
}

func (s *Server) isFlakeDir(dir string) bool {
	path := filepath.Join(dir, nix.FlakeFile)
	source, _, err := regularfile.Read(path, nixeval.MaxFlakeFile)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			s.logger.Warn("reading a file that may be a flake", "path", path, "err", err)
		}
		return false
	}
	return nix.Read(string(source)).IsFlake()
}

func (s *Server) warmFlakeOf(at docstore.Location, force bool) {
	if l, ok := host.FromName(at.Path); !at.Local || !ok || l != host.Nix {
		return
	}
	if settings, _ := s.settingsFor(at); !settings.Nix.Evaluate {
		return
	}
	dir, ok := s.nixFlakeDir(at.Path)
	if !ok {
		return
	}
	if session := s.nixSession(); session != nil {
		s.warmFlake(session, dir, force)
	}
}

func (s *Server) warmFlake(session *nixSession, dir string, force bool) {
	if !session.claimWarm(dir, force) {
		return
	}
	session.watch(session.client.Warm(dir), func(err error) {
		session.warmFinished(dir)
		s.nixProblem(err)
	})
}

func (s *Server) nixProblem(err error) {
	if err == nil || errors.Is(err, nixeval.ErrSuperseded) || errors.Is(err, nixeval.ErrClosed) || errors.Is(err, nixeval.ErrNoNixpkgsInput) || errors.Is(err, context.Canceled) {
		return
	}
	s.logger.Warn("nix evaluation failed", "err", err)
	message := "Nix evaluation failed: " + strings.ToValidUTF8(err.Error()[:min(len(err.Error()), maxNixNoticeBytes)], "")
	if _, missing := errors.AsType[*nixeval.NixNotFoundError](err); missing {
		message = nixMissingNotice
	}
	if s.notices.add("nixeval:"+message, struct{}{}) {
		s.notify(showMessageMethod, lsp.ShowMessageParams{Type: lsp.MessageWarning, Message: message})
	}
}

type nixRequest struct {
	server  *Server
	session *nixSession
	parent  context.Context
	dir     string
	classes func() []nix.OptionClass

	deadline   context.Context
	cancel     context.CancelFunc
	incomplete bool
	sources    []nixeval.OptionSource
	loaded     bool
	trees      map[nixeval.OptionSource]*nixeval.OptionTree
	base       *nixeval.OptionTree
}

func (s *Server) nixEvaluated(ctx context.Context, docs *docstore.View, snap *snapshot) *nixRequest {
	r := &nixRequest{server: s, parent: ctx, trees: map[nixeval.OptionSource]*nixeval.OptionTree{}}
	if !snap.settings.Nix.Evaluate || !snap.Doc.Local {
		if !s.nixEvaluationWanted() {
			s.closeNixSession(false)
		}
		return r
	}
	dir, ok := s.nixFlakeDir(snap.Path)
	if !ok {
		return r
	}
	session := s.nixSession()
	if session == nil {
		return r
	}
	s.warmFlake(session, dir, false)
	r.session, r.dir = session, dir
	r.classes = sync.OnceValue(func() []nix.OptionClass { return r.fileClasses(docs, snap) })
	return r
}

func (r *nixRequest) fileClasses(docs *docstore.View, snap *snapshot) []nix.OptionClass {
	s := r.server
	lookup := r.session.classLookup(snap.URI, s.hostStamp(docs, host.Nix), func(ctx context.Context) []nix.OptionClass {
		return s.nixFileClasses(ctx, docs, snap)
	})
	patience := time.NewTimer(cmp.Or(s.nixEval.classPatience, nixClassPatience))
	defer patience.Stop()
	select {
	case <-lookup.done:
		return lookup.settled()
	case <-patience.C:
	case <-r.parent.Done():
	}
	r.incomplete = true
	return lookup.settled()
}

func (s *Server) nixFileClasses(ctx context.Context, docs *docstore.View, snap *snapshot) []nix.OptionClass {
	h, ok := hostnav.Of(snap.File)
	if !ok {
		return nil
	}
	g, err := s.group(ctx, docs, h)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.Warn("finding the module classes of a Nix file", "path", snap.Path, "err", err)
		}
		return nil
	}
	return g.NixClasses()
}

func (r *nixRequest) done() {
	if r.cancel != nil {
		r.cancel()
	}
}

func (r *nixRequest) ctx() context.Context {
	if r.deadline == nil {
		r.deadline, r.cancel = context.WithTimeout(r.parent, nixRequestDeadline)
	}
	return r.deadline
}

func pendingRefresh(refresh *nixeval.Refresh) bool {
	select {
	case <-refresh.Done():
		return false
	default:
		return true
	}
}

func nixValue[T any](r *nixRequest, answer nixeval.Answer[T], err error) (T, bool) {
	value, stale := answer.Get()
	if err != nil {
		r.failed(err)
		return value, false
	}
	if stale != nil {
		r.waitFor(stale.Refresh)
		if pendingRefresh(stale.Refresh) {
			r.server.warmFlake(r.session, r.dir, true)
		}
	}
	return value, true
}

func (r *nixRequest) waitFor(refresh *nixeval.Refresh) {
	r.incomplete = r.incomplete || pendingRefresh(refresh)
	r.session.watch(refresh, r.server.nixProblem)
}

func (r *nixRequest) failed(err error) {
	if notReady, ok := errors.AsType[*nixeval.NotReadyError](err); ok {
		r.incomplete = true
		r.session.watch(notReady.Refresh, r.server.nixProblem)
		return
	}
	if errors.Is(err, context.DeadlineExceeded) && r.parent.Err() == nil {
		r.incomplete = true
		return
	}
	r.server.nixProblem(err)
}

func (r *nixRequest) optionSources() ([]nixeval.OptionSource, bool) {
	if !r.loaded {
		answer, err := r.session.client.OptionSources(r.ctx(), r.dir)
		sources, ok := nixValue(r, answer, err)
		if !ok {
			return nil, false
		}
		r.sources, r.loaded = sources, true
	}
	return r.sources, true
}

type optionRoot struct {
	source string
	tree   *nixeval.OptionTree
	prefix []string
}

func (r *nixRequest) sourceTree(source nixeval.OptionSource) *nixeval.OptionTree {
	if tree, ok := r.trees[source]; ok {
		return tree
	}
	answer, err := r.session.client.Options(r.ctx(), r.dir, source)
	tree, ok := nixValue(r, answer, err)
	if !ok {
		return nil
	}
	r.trees[source] = tree
	return tree
}

func (r *nixRequest) baseTree() *nixeval.OptionTree {
	if r.base == nil {
		answer, err := r.session.client.BaseOptions(r.ctx(), r.dir)
		if tree, ok := nixValue(r, answer, err); ok {
			r.base = tree
		}
	}
	return r.base
}

func sourceLabel(source nixeval.OptionSource) string {
	return strings.TrimSpace(source.Kind.String() + " " + source.Name)
}

func (r *nixRequest) classesFor(class nix.OptionClass) []nix.OptionClass {
	if class != nix.ClassUnknown {
		return []nix.OptionClass{class}
	}
	classes := r.classes()
	if len(classes) == 0 || slices.Contains(classes, nix.ClassUnknown) {
		return []nix.OptionClass{nix.ClassUnknown}
	}
	return classes
}

func (r *nixRequest) rootsOf(class nix.OptionClass) []optionRoot {
	if r.session == nil {
		return nil
	}
	classes := r.classesFor(class)
	sources, ok := r.optionSources()
	if !ok {
		return nil
	}
	var out []optionRoot
	for _, c := range classes {
		out = append(out, r.classRoots(c, sources)...)
	}
	return out
}

func (r *nixRequest) classRoots(class nix.OptionClass, sources []nixeval.OptionSource) []optionRoot {
	var out []optionRoot
	add := func(label string, tree *nixeval.OptionTree, prefix []string) {
		if tree != nil {
			out = append(out, optionRoot{source: label, tree: tree, prefix: prefix})
		}
	}
	hasNixOS := false
	for _, source := range sources {
		switch source.Kind {
		case nixeval.SourceNixOS:
			hasNixOS = true
			if class == nix.ClassNixOS || class == nix.ClassUnknown {
				add(sourceLabel(source), r.sourceTree(source), nil)
			}
			if class == nix.ClassHomeManager || class == nix.ClassUnknown {
				add("home-manager in "+sourceLabel(source), r.sourceTree(source), nixUserOptions)
			}
		case nixeval.SourceHomeManager:
			if class == nix.ClassHomeManager || class == nix.ClassUnknown {
				add(sourceLabel(source), r.sourceTree(source), nil)
			}
		case nixeval.SourceFlakeParts:
			if class == nix.ClassFlakeParts || class == nix.ClassUnknown {
				add(sourceLabel(source), r.sourceTree(source), nil)
			}
			if class == nix.ClassPerSystem {
				add(sourceLabel(source), r.sourceTree(source), []string{"perSystem"})
			}
		}
	}
	if class == nix.ClassNixOS && !hasNixOS {
		add("NixOS options of the locked nixpkgs", r.baseTree(), nil)
	}
	return out
}

func appendSource(sources []string, source string) []string {
	if slices.Contains(sources, source) {
		return sources
	}
	return append(sources, source)
}

func (r *nixRequest) OptionChildren(site nix.OptionSite) []nix.OptionChild {
	merged := map[string]*nix.OptionChild{}
	for _, root := range r.rootsOf(site.Class) {
		node, ok := root.tree.Lookup(slices.Concat(root.prefix, site.Path))
		if !ok {
			continue
		}
		for child := range node.Children() {
			if child.IsPlaceholder() {
				continue
			}
			name := child.Name()
			entry, seen := merged[name]
			if !seen {
				entry = &nix.OptionChild{Name: name}
				merged[name] = entry
			}
			entry.Sources = appendSource(entry.Sources, root.source)
			if option, ok := child.Option(); ok && entry.Option == nil {
				entry.Option = &option
			}
		}
	}
	out := make([]nix.OptionChild, 0, len(merged))
	for _, name := range slices.Sorted(maps.Keys(merged)) {
		out = append(out, *merged[name])
	}
	return out
}

func (r *nixRequest) OptionAt(site nix.OptionSite) (nix.SourcedOption, bool) {
	var found nix.SourcedOption
	for _, root := range r.rootsOf(site.Class) {
		node, ok := root.tree.Lookup(slices.Concat(root.prefix, site.Path))
		if !ok {
			continue
		}
		option, ok := node.Option()
		if !ok {
			continue
		}
		if found.Sources == nil {
			found.Option = option
		}
		found.Sources = appendSource(found.Sources, root.source)
	}
	return found, found.Sources != nil
}

func (r *nixRequest) NixpkgsChildren(path []string, prefix string) []string {
	if r.session == nil {
		return nil
	}
	answer, err := r.session.client.NixpkgsAttrs(r.ctx(), r.dir, path, prefix)
	names, ok := nixValue(r, answer, err)
	if !ok {
		return nil
	}
	if prefix != "" || len(names) > maxNixpkgsItems {
		r.incomplete = true
	}
	return names[:min(len(names), maxNixpkgsItems)]
}

func (r *nixRequest) NixpkgsPackage(path []string) (*nixeval.Package, bool) {
	if r.session == nil {
		return nil, false
	}
	answer, err := r.session.client.NixpkgsPackage(r.ctx(), r.dir, path)
	pkg, ok := nixValue(r, answer, err)
	return pkg, ok && pkg != nil
}
