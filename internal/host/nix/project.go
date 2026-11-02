package nix

import (
	"cmp"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

const (
	maxProjectFiles = 8192
	maxForeignFiles = 256
	maxFlowRounds   = 8
	maxSpaceDepth   = 32
	maxSpaces       = 1 << 15
	maxFlowSteps    = 1 << 24
	maxPeekedFiles  = 16
	treeExcluded    = "/_"
)

type Source interface {
	LoadAll(paths []string) []*model.Index
	Tree(dir string) ([]string, error)
	Read(path string) ([]byte, error)
}

type Request struct {
	Self     *model.Index
	SelfPath string
	Flake    string
	Roots    []string
	Failure  string
	Home     string
	ScanCut  bool
}

type flowCaller struct {
	unit *symbolTable
	call flowCall
}

type Project struct {
	mu     sync.Mutex
	source Source
	units  []*symbolTable
	paths  []string
	files  map[*symbolTable]int
	byPath map[string]int
	tried  map[string]bool
	trees  map[string][]string

	truncated bool
	failure   string
	scanCut   bool
	home      string
	settled   bool
	foreign   map[int]bool
	peek      Files
	peeks     int

	flakeUnit *symbolTable
	flakeDir  string
	outputs   *syntax.Lambda
	lock      *flakeLock
	lockRead  bool

	memo      map[syntax.Expr][]flowValue
	visiting  map[syntax.Expr]bool
	selecting map[flowSelection]bool
	bindings  map[*scope.Def][]flowValue
	changed   bool

	nextShared  model.Scope
	classes     map[string]*flowClass
	inputs      *space
	memberships map[model.Home][]membership
	seen        map[any]bool
	callers     map[flowLambda][]flowCaller
	sites       map[int][]bool
	modular     map[flowLambda]bool
	slotHomes   map[model.Home][]model.Home
	escapesRead bool
	escapedSets map[*attrs]bool

	spaces        int
	steps         int
	applying      map[*space]bool
	expanding     map[flowValue]bool
	envMemo       map[sinkKey]bool
	overlayValues []flowValue
	collecting    bool
	scriptMemo    map[sinkKey]string
	sinkVisiting  map[sinkKey]bool
	provedCalls   map[*syntax.Apply][]provenString
	provenFiles   map[int][]provenString
	provenScopes  map[provenKey]model.Scope
	provenEnvs    map[model.Home]environment
	spaceClosure  map[spaceQuery][]*space
}

type spaceQuery struct {
	home model.Home
	name string
}

var (
	sessionPaths        = [][]string{{"home", "sessionVariables"}, {"environment", "variables"}, {"environment", "sessionVariables"}}
	installablePrefixes = [][]string{
		nil,
		{"packages"},
		{"legacyPackages"},
		{"apps"},
		{"checks"},
		{"devShells"},
		{"nixosConfigurations"},
		{"homeConfigurations"},
	}
)

var scanOrder = [...][]callKind{
	{callMkFlake},
	{callRoot},
	{callImport, callAuto, callLocal, callMapAttrs, callWithSystem, callFix},
}

type wantedFile struct {
	path     string
	fallback string
}

func Link(req Request, source Source) *Project {
	p := &Project{
		source:       source,
		files:        map[*symbolTable]int{},
		byPath:       map[string]int{},
		tried:        map[string]bool{},
		trees:        map[string][]string{},
		failure:      req.Failure,
		scanCut:      req.ScanCut,
		home:         req.Home,
		foreign:      map[int]bool{},
		applying:     map[*space]bool{},
		expanding:    map[flowValue]bool{},
		sinkVisiting: map[sinkKey]bool{},
		provenScopes: map[provenKey]model.Scope{},
		provenEnvs:   map[model.Home]environment{},
		bindings:     map[*scope.Def][]flowValue{},
		visiting:     map[syntax.Expr]bool{},
		selecting:    map[flowSelection]bool{},
		classes:      map[string]*flowClass{},
		slotHomes:    map[model.Home][]model.Home{},
		escapedSets:  map[*attrs]bool{},
	}
	p.inputs = p.newSpace(nil, inputsAttr, inputsRole)
	next := p.add(req.SelfPath, req.Self)
	for _, root := range req.Roots {
		next = append(next, wantedFile{path: root})
	}
	if req.Flake != "" {
		next = append(next, wantedFile{path: req.Flake})
	}
	p.closure(next)
	if i, ok := p.byPath[req.Flake]; ok && req.Flake != "" {
		p.flakeUnit, p.flakeDir = p.units[i], filepath.Dir(req.Flake)
	}
	p.settle()
	return p
}

type localSource struct{}

func (localSource) LoadAll(paths []string) []*model.Index { return make([]*model.Index, len(paths)) }

func (localSource) Tree(string) ([]string, error) { return nil, nil }

func (localSource) Read(path string) ([]byte, error) {
	return nil, fmt.Errorf("reading %s: a single file has no project to read from", path)
}

func standalone(ix *model.Index) *Project {
	return Link(Request{Self: ix}, localSource{})
}

func (p *Project) Fresh(source Source) bool {
	p.begin()
	defer p.mu.Unlock()
	for i, ix := range source.LoadAll(p.paths[1:]) {
		if ix != p.units[i+1].ix {
			return false
		}
	}
	for root, files := range p.trees {
		listed, err := source.Tree(root)
		if err != nil || !slices.Equal(keptModules(root, listed), files) {
			return false
		}
	}
	return true
}

func (p *Project) Indexes() []*model.Index {
	out := make([]*model.Index, len(p.units))
	for i, u := range p.units {
		out[i] = u.ix
	}
	return out
}

func (p *Project) add(path string, ix *model.Index) []wantedFile {
	r, ok := ix.Renames.(renames)
	if !ok {
		panic("nix: the index of " + path + " was not analyzed as Nix")
	}
	u := r.unit
	p.files[u] = len(p.units)
	p.units = append(p.units, u)
	p.paths = append(p.paths, path)
	if path == "" {
		return nil
	}
	p.byPath[path] = p.files[u]
	if strings.HasPrefix(path, storeDirectory+"/") {
		p.foreign[p.files[u]] = true
		return nil
	}
	return p.edges(u, path)
}

func (p *Project) edges(u *symbolTable, path string) []wantedFile {
	dir := filepath.Dir(path)
	var out []wantedFile
	for _, inc := range u.ix.Includes {
		if inc.Probe {
			continue
		}
		wanted := wantedFile{path: p.located(dir, inc.Candidates[0])}
		if len(inc.Candidates) > 1 {
			wanted.fallback = p.located(dir, inc.Candidates[1])
		}
		out = append(out, wanted)
	}
	for _, tree := range u.trees {
		if !local(tree) {
			continue
		}
		root := p.resolve(u, tree)
		for _, file := range p.treeFiles(root) {
			out = append(out, wantedFile{path: file})
		}
	}
	return out
}

func (p *Project) treeFiles(root string) []string {
	if files, done := p.trees[root]; done {
		return files
	}
	listed, err := p.source.Tree(root)
	if err != nil {
		p.failure = fmt.Sprintf("listing the modules under %s: %v", root, err)
	}
	files := keptModules(root, listed)
	p.trees[root] = files
	return files
}

func keptModules(root string, listed []string) []string {
	return slices.DeleteFunc(listed, func(file string) bool {
		rel := strings.TrimPrefix(file, root)
		return !strings.HasSuffix(rel, nixExtension) || strings.Contains(rel, treeExcluded)
	})
}

func within(dir, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(dir, path)
}

func (p *Project) located(dir, path string) string {
	if rest, home := strings.CutPrefix(path, "~/"); home && p.home != "" {
		return filepath.Join(p.home, rest)
	}
	return within(dir, path)
}

func (p *Project) resolve(u *symbolTable, path *syntax.Path) string {
	span := path.Span()
	return p.located(filepath.Dir(p.paths[p.files[u]]), u.f.src[span.Start:span.End])
}

func (p *Project) closure(level []wantedFile) {
	for len(level) > 0 {
		var wanted []wantedFile
		var paths []string
		for _, w := range level {
			if !p.tried[w.path] {
				p.tried[w.path] = true
				if _, known := p.byPath[w.path]; !known {
					wanted = append(wanted, w)
					paths = append(paths, w.path)
				}
			}
		}
		var next []wantedFile
		for i, ix := range p.source.LoadAll(paths) {
			if ix == nil {
				if wanted[i].fallback != "" {
					next = append(next, wantedFile{path: wanted[i].fallback})
				}
				continue
			}
			if strings.HasPrefix(wanted[i].path, storeDirectory+"/") {
				if len(p.foreign) >= maxForeignFiles {
					continue
				}
			} else if len(p.units)-len(p.foreign) >= maxProjectFiles {
				p.truncated = true
				return
			}
			next = append(next, p.add(wanted[i].path, ix)...)
		}
		level = next
	}
}

func (p *Project) settle() {
	for range maxFlowRounds {
		p.reset()
		if p.flakeUnit != nil {
			p.flakeRound()
		}
		for _, kinds := range scanOrder {
			p.memo = map[syntax.Expr][]flowValue{}
			for _, u := range p.units {
				for _, c := range u.flowCalls {
					if slices.Contains(kinds, c.kind) {
						p.scan(u, c)
					}
				}
			}
		}
		p.readDependencies()
		if !p.changed {
			break
		}
	}
	if p.changed {
		p.failure = cmp.Or(p.failure, fmt.Sprintf("following values across files did not settle within %d rounds", maxFlowRounds))
	}
	p.clearDerived()
	p.settled = true
}

func (p *Project) enter(v flowValue) bool {
	if p.expanding[v] || !p.spend() {
		return false
	}
	p.expanding[v] = true
	return true
}

func (p *Project) begin() {
	p.mu.Lock()
	p.steps = 0
}

func (p *Project) spend() bool {
	p.steps++
	if p.steps <= maxFlowSteps {
		return true
	}
	p.failure = cmp.Or(p.failure, fmt.Sprintf("following values took more than %d steps, so it stopped", maxFlowSteps))
	return false
}

func compactHomes(homes []model.Home) []model.Home {
	slices.SortFunc(homes, func(a, b model.Home) int {
		return cmp.Or(cmp.Compare(a.File, b.File), cmp.Compare(a.Scope, b.Scope))
	})
	return slices.Compact(homes)
}

func (p *Project) clearDerived() {
	p.steps = 0
	p.memo = map[syntax.Expr][]flowValue{}
	p.overlayValues = nil
	p.scriptMemo = map[sinkKey]string{}
	p.envMemo = map[sinkKey]bool{}
	p.provedCalls = map[*syntax.Apply][]provenString{}
	p.provenFiles = map[int][]provenString{}
	p.spaceClosure = map[spaceQuery][]*space{}
}

func (p *Project) readDependencies() {
	for i := 0; i < len(p.units); i++ {
		if p.foreign[i] {
			continue
		}
		u := p.units[i]
		for _, id := range slices.Sorted(maps.Keys(u.slots)) {
			p.slotValues(u, u.slots[id])
		}
	}
}

func (p *Project) reset() {
	p.changed = false
	p.clearDerived()
	p.memberships = map[model.Home][]membership{}
	p.seen = map[any]bool{}
	p.callers = map[flowLambda][]flowCaller{}
	p.sites = map[int][]bool{}
	p.modular = map[flowLambda]bool{}
	p.inputs.clear()
	for _, c := range p.classes {
		c.root.clear()
	}
}

func (p *Project) bind(d *scope.Def, values []flowValue) {
	for _, v := range values {
		if !slices.Contains(p.bindings[d], v) {
			p.bindings[d] = append(p.bindings[d], v)
			p.changed = true
		}
	}
}

func (p *Project) bindParam(fn flowLambda, values []flowValue) {
	table := fn.unit.f.table
	switch param := fn.fn.Param.(type) {
	case *syntax.IdentParam:
		if d, ok := table.Binder(param.Name); ok {
			p.bind(d, values)
		}
	case *syntax.PatternParam:
		for _, formal := range param.Formals {
			if d, ok := table.Binder(formal.Name); ok {
				p.bind(d, p.selectAll(values, formal.Name.Name))
			}
		}
		if param.Alias == nil {
			return
		}
		if d, ok := table.Binder(param.Alias); ok {
			p.bind(d, values)
		}
	}
}

func (p *Project) applyBinding(u *symbolTable, values []flowValue, args []syntax.Expr) {
	for _, arg := range args {
		given := p.eval(u, arg)
		var next []flowValue
		for _, v := range values {
			if fn, ok := v.(flowLambda); ok {
				p.bindParam(fn, given)
				next = union(next, p.eval(fn.unit, fn.fn.Body))
			}
		}
		values = next
	}
}

func (p *Project) scan(u *symbolTable, c flowCall) {
	args := c.call.Args
	switch c.kind {
	case callFix:
		for _, v := range p.eval(u, args[len(args)-1]) {
			if fn, ok := v.(flowLambda); ok {
				p.bindParam(fn, p.eval(fn.unit, fn.fn.Body))
			}
		}
	case callImport, callAuto:
		p.scanImport(u, c)
	case callLocal:
		p.applyBinding(u, p.eval(u, c.call.Fn), args)
	case callMkFlake:
		if len(args) > 1 {
			p.walkAll(p.class(classFlake), p.evalModuleValue(u, args[1]))
		}
	case callRoot:
		p.scanRoot(u, c)
	case callMapAttrs:
		if len(args) > 1 {
			p.scanMapper(u, args)
		}
	case callWithSystem:
		if c.path < len(args) {
			p.scanWithSystem(u, args[c.path])
		}
	}
}

func (p *Project) scanImport(u *symbolTable, c flowCall) {
	if at, ok := p.resolvedArg(u, c); ok {
		i, known := p.fileIndex(at)
		if known && c.kind == callImport {
			p.sites[i] = append(p.sites[i], c.closed)
		}
	}
	values := p.importValue(u, c.call.Args[c.path])
	for _, v := range values {
		if fn, ok := v.(flowLambda); ok {
			p.callers[fn] = append(p.callers[fn], flowCaller{unit: u, call: c})
			if c.kind == callAuto {
				p.fillFromPackages(fn)
			}
		}
	}
	p.applyBinding(u, values, c.call.Args[c.path+1:])
}

func (p *Project) fillFromPackages(fn flowLambda) {
	param, pattern := fn.fn.Param.(*syntax.PatternParam)
	if !pattern {
		return
	}
	for _, formal := range param.Formals {
		if d, ok := fn.unit.f.table.Binder(formal.Name); ok {
			p.bind(d, p.selectFrom(flowPkgs{}, formal.Name.Name))
		}
	}
}

func (p *Project) resolvedArg(u *symbolTable, c flowCall) (string, bool) {
	for _, v := range p.eval(u, c.call.Args[c.path]) {
		if path, ok := v.(flowPath); ok {
			return path.path, true
		}
	}
	return "", false
}

func (p *Project) scanMapper(u *symbolTable, args []syntax.Expr) {
	elements := []flowValue{}
	for _, v := range p.eval(u, args[1]) {
		elements = union(elements, p.children(v))
	}
	for _, v := range p.eval(u, args[0]) {
		name, ok := v.(flowLambda)
		if !ok {
			continue
		}
		for _, inner := range p.eval(name.unit, name.fn.Body) {
			if fn, ok := inner.(flowLambda); ok {
				p.bindParam(fn, elements)
			}
		}
	}
}

func (p *Project) scanWithSystem(u *symbolTable, arg syntax.Expr) {
	c := p.class(classPerSystem)
	for _, v := range p.eval(u, arg) {
		if fn, ok := v.(flowLambda); ok {
			p.moduleArgs(c, fn)
		}
	}
}

func (p *Project) Homes(file int, scope model.Scope, name string) ([]model.Home, bool) {
	p.begin()
	defer p.mu.Unlock()
	return p.homes(file, scope, name)
}

func (p *Project) homes(file int, scope model.Scope, name string) ([]model.Home, bool) {
	at := model.Home{File: file, Scope: scope}
	if slot, ok := p.units[file].slots[scope]; ok {
		return p.homesOfSlot(at, slot), true
	}
	if env, ok := p.shellEnv(at); ok {
		out := p.shellHomes(at, env, name)
		return out, len(out) > 0
	}
	var out []model.Home
	for _, s := range p.spacesOf(at, name) {
		out = append(out, s.members...)
	}
	return compactHomes(out), len(out) > 0
}

func (p *Project) shellEnv(at model.Home) (environment, bool) {
	if env, ok := p.units[at.File].shellEnvs[at.Scope]; ok {
		return env, true
	}
	env, ok := p.provenEnvs[at]
	return env, ok
}

func (p *Project) shellHomes(at model.Home, env environment, name string) []model.Home {
	if slices.Contains(unexported, name) || strings.HasPrefix(name, "__") {
		return nil
	}
	u := p.units[at.File]
	homes, done := p.slotHomes[at]
	if !done {
		for _, set := range u.ix.Extends[at.Scope] {
			homes = append(homes, model.Home{File: at.File, Scope: set})
		}
		var values []flowValue
		if own := u.sets[env.set]; own != nil {
			values = []flowValue{flowSet{unit: u, set: own}}
			for _, attr := range env.attrPath() {
				values = p.selectAll(values, attr)
			}
			if len(env.attrPath()) == 0 {
				values = nil
			}
		}
		if env.expr != nil {
			values = union(values, p.evalModuleValue(u, env.expr))
		}
		for _, v := range values {
			homes = p.place(homes, v)
		}
		homes = compactHomes(homes)
		p.slotHomes[at] = homes
	}
	if slices.ContainsFunc(homes, func(h model.Home) bool { return p.holds(h, name) }) {
		return homes
	}
	return p.sessionHomes(name)
}

func (p *Project) holds(h model.Home, name string) bool {
	return slices.ContainsFunc(p.units[h.File].ix.Symbols, func(s model.Symbol) bool { return s.Scope == h.Scope && s.Name == name })
}

func (p *Project) sessionHomes(name string) []model.Home {
	var out []model.Home
	for _, class := range sortedNames(p.classes) {
		for _, path := range sessionPaths {
			at := p.classes[class].root
			for _, attr := range path {
				if at != nil {
					at = at.kids[attr]
				}
			}
			if at != nil && at.names[name] {
				out = append(out, at.members...)
			}
		}
	}
	return out
}

func (p *Project) spacesOf(at model.Home, name string) []*space {
	key := spaceQuery{home: at, name: name}
	if done, ok := p.spaceClosure[key]; ok {
		return done
	}
	var out []*space
	queue := []model.Home{at}
	seen := map[model.Home]bool{at: true}
	for len(queue) > 0 {
		h := queue[0]
		queue = queue[1:]
		for _, m := range p.memberships[h] {
			if !m.applies(name) || slices.Contains(out, m.space) {
				continue
			}
			out = append(out, m.space)
			for _, member := range m.space.members {
				if !seen[member] && p.holds(member, name) {
					seen[member] = true
					queue = append(queue, member)
				}
			}
		}
	}
	slices.SortFunc(out, func(a, b *space) int { return cmp.Compare(b.id, a.id) })
	p.spaceClosure[key] = out
	return out
}

func (p *Project) Installable(path []string) []model.Home {
	p.begin()
	defer p.mu.Unlock()
	if len(path) == 0 {
		return nil
	}
	roots := p.selfValues()
	if c, ok := p.classes[classPerSystem]; ok {
		roots = union(roots, []flowValue{flowSpace{space: c.root}})
	}
	last := path[len(path)-1]
	for _, prefix := range installablePrefixes {
		values := roots
		for _, attr := range prefix {
			var next []flowValue
			for _, v := range p.selectAll(values, attr) {
				next = union(next, p.collapsed(v))
			}
			values = next
		}
		for _, attr := range path[:len(path)-1] {
			values = p.selectAll(values, attr)
		}
		var homes []model.Home
		for _, v := range values {
			homes = p.place(homes, v)
		}
		homes = slices.DeleteFunc(homes, func(h model.Home) bool { return !p.holds(h, last) })
		if len(homes) > 0 {
			return homes
		}
	}
	return nil
}

func (p *Project) Package(name string) []model.Home {
	p.begin()
	defer p.mu.Unlock()
	var homes []model.Home
	for _, body := range p.overlayBodies() {
		homes = p.place(homes, body)
	}
	return slices.DeleteFunc(homes, func(h model.Home) bool { return !p.holds(h, name) })
}

func (p *Project) Shared(file int, scope model.Scope, name string) (model.Home, bool) {
	p.begin()
	defer p.mu.Unlock()
	if spaces := p.spacesOf(model.Home{File: file, Scope: scope}, name); len(spaces) > 0 {
		return model.Home{File: sharedFile, Scope: spaces[0].id}, true
	}
	return model.Home{}, false
}

func (p *Project) homesOfSlot(at model.Home, slot flowSlot) []model.Home {
	if done, ok := p.slotHomes[at]; ok {
		return done
	}
	u := p.units[at.File]
	var out []model.Home
	for _, v := range p.slotValues(u, slot) {
		if fn, called := v.(flowLambda); called && slot.call != nil {
			out = append(out, model.Home{File: p.files[fn.unit], Scope: fn.unit.formals[fn.fn].id})
			continue
		}
		out = p.place(out, v)
	}
	out = compactHomes(out)
	p.slotHomes[at] = out
	return out
}

func (p *Project) slotValues(u *symbolTable, slot flowSlot) []flowValue {
	if slot.call != nil {
		return p.calleeAt(u, slot.call, slot.arg)
	}
	var out []flowValue
	for _, target := range slot.targets {
		values := p.eval(u, target)
		for _, name := range slot.path {
			values = p.selectAll(values, name)
		}
		out = union(out, values)
	}
	return out
}

func (p *Project) calleeAt(u *symbolTable, call *syntax.Apply, arg int) []flowValue {
	fn := syntax.Unparenthesized(call.Fn)
	values, skip := []flowValue(nil), 0
	switch name := calleeName(fn); {
	case name == importName && u.builtinImport(fn), name == callPackageName:
		values, skip = p.importValue(u, call.Args[0]), 1
	case name == callPackageWithName && len(call.Args) > 1:
		values, skip = p.importValue(u, call.Args[1]), 2
	default:
		values = p.eval(u, fn)
	}
	return p.applied(values, arg-skip)
}

func (p *Project) place(out []model.Home, v flowValue) []model.Home {
	if !p.enter(v) {
		return out
	}
	defer delete(p.expanding, v)
	switch v := v.(type) {
	case flowSet:
		home := model.Home{File: p.files[v.unit], Scope: v.set.id}
		members := p.memberships[home]
		if len(members) == 0 {
			return append(out, home)
		}
		for _, m := range members {
			out = append(out, m.space.members...)
		}
		return out
	case flowSpace:
		return append(out, v.space.members...)
	case flowPrimed:
		return append(out, v.space.members...)
	case flowSelf:
		for _, value := range p.selfValues() {
			out = p.place(out, value)
		}
	case flowPkgs:
		for _, body := range p.overlayBodies() {
			out = p.place(out, body)
		}
	case flowUpdate:
		for _, side := range []syntax.Expr{v.node.Left, v.node.Right} {
			for _, value := range p.eval(v.unit, side) {
				out = p.place(out, value)
			}
		}
	case flowMapped:
		for _, value := range p.eval(v.unit, v.call.Args[1]) {
			out = p.place(out, value)
		}
	}
	return out
}

func sortedNames[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}
