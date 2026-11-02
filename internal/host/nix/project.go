package nix

import (
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
	maxFlowRounds   = 8
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
}

var scanOrder = [...][]callKind{
	{callMkFlake},
	{callRoot},
	{callImport, callAuto, callLocal, callMapAttrs, callWithSystem},
}

type wantedFile struct {
	path     string
	fallback string
}

func Link(req Request, source Source) *Project {
	p := &Project{
		source:      source,
		files:       map[*symbolTable]int{},
		byPath:      map[string]int{},
		tried:       map[string]bool{},
		trees:       map[string][]string{},
		failure:     req.Failure,
		bindings:    map[*scope.Def][]flowValue{},
		visiting:    map[syntax.Expr]bool{},
		selecting:   map[flowSelection]bool{},
		classes:     map[string]*flowClass{},
		slotHomes:   map[model.Home][]model.Home{},
		escapedSets: map[*attrs]bool{},
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

func (p *Project) Fresh(source Source) bool {
	p.mu.Lock()
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
	return p.edges(u, path)
}

func (p *Project) edges(u *symbolTable, path string) []wantedFile {
	dir := filepath.Dir(path)
	var out []wantedFile
	for _, inc := range u.ix.Includes {
		if inc.Probe {
			continue
		}
		wanted := wantedFile{path: within(dir, inc.Candidates[0])}
		if len(inc.Candidates) > 1 {
			wanted.fallback = within(dir, inc.Candidates[1])
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

func (p *Project) resolve(u *symbolTable, path *syntax.Path) string {
	span := path.Span()
	return within(filepath.Dir(p.paths[p.files[u]]), u.f.src[span.Start:span.End])
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
			if len(p.units) >= maxProjectFiles {
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
		if !p.changed {
			break
		}
	}
	p.memo = map[syntax.Expr][]flowValue{}
}

func (p *Project) reset() {
	p.changed = false
	p.memo = map[syntax.Expr][]flowValue{}
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
		if len(args) > 1 {
			p.scanWithSystem(u, args[1])
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
		}
	}
	p.applyBinding(u, values, c.call.Args[c.path+1:])
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
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.homes(file, scope, name)
}

func (p *Project) homes(file int, scope model.Scope, name string) ([]model.Home, bool) {
	at := model.Home{File: file, Scope: scope}
	if slot, ok := p.units[file].slots[scope]; ok {
		return p.homesOfSlot(at, slot), true
	}
	var out []model.Home
	for _, m := range p.memberships[at] {
		if m.applies(name) {
			out = append(out, m.space.members...)
		}
	}
	return out, len(out) > 0
}

func (p *Project) Shared(file int, scope model.Scope, name string) (model.Home, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, m := range p.memberships[model.Home{File: file, Scope: scope}] {
		if m.applies(name) {
			return model.Home{File: sharedFile, Scope: m.space.id}, true
		}
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
	switch calleeName(fn) {
	case importName, callPackageName:
		values, skip = p.importValue(u, call.Args[0]), 1
	case callPackageWithName:
		values, skip = p.importValue(u, call.Args[1]), 2
	default:
		values = p.eval(u, fn)
	}
	return p.applied(values, arg-skip)
}

func (p *Project) place(out []model.Home, v flowValue) []model.Home {
	switch v := v.(type) {
	case flowSet:
		home := model.Home{File: p.files[v.unit], Scope: v.set.id}
		if members := p.memberships[home]; len(members) > 0 {
			return append(out, members[0].space.members...)
		}
		return append(out, home)
	case flowSpace:
		return append(out, v.space.members...)
	case flowSelf:
		for _, value := range p.selfValues() {
			out = p.place(out, value)
		}
	}
	return out
}

func sortedNames[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}
