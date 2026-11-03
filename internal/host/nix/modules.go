package nix

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

const (
	classFlake     = "flake"
	classPerSystem = "perSystem"
	classNixos     = "nixos"
	classHome      = "homeManager"
	classDarwin    = "darwin"
	classDroid     = "nixOnDroid"
	classModules   = "modules"
	classSubmodule = "submodule"

	configFormal  = "config"
	optionsFormal = "options"
	inputsPrimed  = "inputs'"
	selfPrimed    = "self'"
	selfFormal    = "self"
	outputsAttr   = "outputs"
	moduleArgsSet = "_module"
	argsAttr      = "args"
	homeManager   = "home-manager"
	usersAttr     = "users"
	sharedAttr    = "sharedModules"
	specialArgs   = "specialArgs"
	extraSpecial  = "extraSpecialArgs"
	anyName       = "*"
	sharedFile    = -1
)

var (
	rootClasses = map[string]string{
		"nixosSystem":              classNixos,
		"homeManagerConfiguration": classHome,
		"darwinSystem":             classDarwin,
		"nixOnDroidConfiguration":  classDroid,
		"evalModules":              classModules,
	}
	moduleKeys    = []string{importsAttr, optionsFormal, configFormal, "_file", "key", "disabledModules", "freeformType"}
	givenArgs     = []string{configFormal, optionsFormal, "lib", pkgsFormal, "modulesPath", specialArgs, inputsAttr, selfFormal, "system", selfPrimed, inputsPrimed, withSystemName, moduleWithSystemName, "flake-parts-lib", "getSystem", "final"}
	wrappingTypes = []string{"listOf", "nullOr", "uniq", "unique", "coercedTo"}
	keyedTypes    = []string{"attrsOf", "lazyAttrsOf"}
)

type moduleOutput struct {
	name   string
	class  string
	single bool
}

var moduleOutputs = []moduleOutput{
	{name: "nixosModules", class: classNixos},
	{name: "homeManagerModules", class: classHome},
	{name: "homeModules", class: classHome},
	{name: "darwinModules", class: classDarwin},
	{name: "flakeModules", class: classFlake},
	{name: "nixosModule", class: classNixos, single: true},
	{name: "homeManagerModule", class: classHome, single: true},
	{name: "darwinModule", class: classDarwin, single: true},
	{name: "flakeModule", class: classFlake, single: true},
}

type spaceRole uint8

const (
	optionRole spaceRole = iota
	argsRole
	inputsRole
)

type flowBound struct {
	unit   *symbolTable
	source flowSource
}

type space struct {
	parent   *space
	name     string
	role     spaceRole
	id       model.Scope
	kids     map[string]*space
	names    map[string]bool
	members  []model.Home
	bound    []flowBound
	sets     []flowSet
	declared bool
	escaped  bool
	leftIn   *symbolTable
	leftAt   int
	dynamic  bool
	module   bool
	sub      *flowClass
	depth    int
}

func (s *space) clear() {
	s.names = map[string]bool{}
	s.members, s.bound, s.sets = nil, nil, nil
	s.declared, s.module = false, false
	for _, kid := range s.kids {
		kid.clear()
	}
}

func (s *space) path() []string {
	var out []string
	for at := s; at.parent != nil; at = at.parent {
		out = append(out, at.name)
	}
	slices.Reverse(out)
	return out
}

type membership struct {
	space   *space
	formals bool
}

func (m membership) applies(name string) bool {
	if m.space.parent == nil && m.space.role == optionRole && slices.Contains(moduleKeys, name) {
		return false
	}
	return !m.formals || m.space.names[name]
}

type flowClass struct {
	name string
	root *space
	args *space
}

func (p *Project) newSpace(parent *space, name string, role spaceRole) *space {
	p.nextShared--
	p.spaces++
	depth := 0
	if parent != nil {
		depth = parent.depth + 1
	}
	return &space{parent: parent, name: name, role: role, id: p.nextShared, kids: map[string]*space{}, names: map[string]bool{}, depth: depth}
}

func (p *Project) tooDeep(s *space) bool {
	if p.spaces >= maxSpaces {
		p.failure = cmp.Or(p.failure, fmt.Sprintf("the project has more than %d attribute paths in its modules, so they are followed no further", maxSpaces))
		return true
	}
	if s.depth < maxSpaceDepth {
		return false
	}
	p.failure = cmp.Or(p.failure, fmt.Sprintf("an attribute set holds itself deeper than %d levels at `%s`, so it is followed no further", maxSpaceDepth, strings.Join(s.path(), ".")))
	return true
}

func (p *Project) kid(s *space, name string) *space {
	kid, ok := s.kids[name]
	if !ok {
		kid = p.newSpace(s, name, s.role)
		s.kids[name] = kid
	}
	return kid
}

func (p *Project) class(name string) *flowClass {
	c, ok := p.classes[name]
	if !ok {
		root := p.newSpace(nil, "", optionRole)
		args := p.kid(p.kid(root, moduleArgsSet), argsAttr)
		args.role = argsRole
		c = &flowClass{name: name, root: root, args: args}
		p.classes[name] = c
	}
	return c
}

func (p *Project) addMember(s *space, u *symbolTable, set *attrs, formals bool) {
	home := model.Home{File: p.files[u], Scope: set.id}
	s.members = append(s.members, home)
	p.memberships[home] = slices.Insert(p.memberships[home], 0, membership{space: s, formals: formals})
	if !formals {
		s.sets = append(s.sets, flowSet{unit: u, set: set})
	}
}

func (p *Project) flakeRound() {
	u := p.flakeUnit
	root, isSet := syntax.Unparenthesized(u.f.tree.Root).(*syntax.AttrSet)
	if !isSet || u.sets[root] == nil {
		return
	}
	set := u.sets[root]
	if inputs := set.kids[inputsAttr]; inputs != nil {
		p.addMember(p.inputs, u, inputs, false)
		for _, name := range inputs.names.keys {
			p.inputs.names[name] = true
		}
	}
	for _, src := range set.sources.of(outputsAttr) {
		if fn, ok := syntax.Unparenthesized(src.expr).(*syntax.Lambda); ok {
			p.outputs = fn
			p.outputsRound(u, fn)
		}
	}
}

func (p *Project) outputsRound(u *symbolTable, fn *syntax.Lambda) {
	p.addMember(p.inputs, u, u.formals[fn], true)
	table := u.f.table
	everything := []flowValue{flowSpace{space: p.inputs}}
	switch param := fn.Param.(type) {
	case *syntax.IdentParam:
		if d, ok := table.Binder(param.Name); ok {
			p.bind(d, everything)
		}
	case *syntax.PatternParam:
		for _, formal := range param.Formals {
			d, ok := table.Binder(formal.Name)
			if !ok {
				continue
			}
			if formal.Name.Name == selfFormal {
				p.bind(d, []flowValue{flowSelf{}})
				continue
			}
			p.bind(d, []flowValue{flowSpace{space: p.kid(p.inputs, formal.Name.Name)}})
		}
		if param.Alias != nil {
			if d, ok := table.Binder(param.Alias); ok {
				p.bind(d, everything)
			}
		}
	}
	for _, out := range p.eval(u, fn.Body) {
		p.moduleOutputs(out)
	}
}

func (p *Project) moduleOutputs(out flowValue) {
	for _, o := range moduleOutputs {
		values := p.selectFrom(out, o.name)
		c := p.class(o.class)
		if o.single {
			p.walkAll(c, values)
			continue
		}
		for _, v := range values {
			p.walkAll(c, p.children(v))
		}
	}
}

func (p *Project) scanRoot(u *symbolTable, call flowCall) {
	c := p.class(call.class)
	for _, arg := range call.call.Args {
		for _, v := range p.eval(u, arg) {
			p.walkAll(c, p.selectFrom(v, modulesAttr))
			for _, name := range []string{specialArgs, extraSpecial} {
				for _, given := range p.selectFrom(v, name) {
					if set, ok := given.(flowSet); ok {
						p.register(c, c.args, set.unit, set.set, false)
					}
				}
			}
		}
	}
}

func (p *Project) walkAll(c *flowClass, values []flowValue) {
	for _, v := range values {
		p.walk(c, v)
	}
}

type walkKey struct {
	class *flowClass
	value flowValue
}

func (p *Project) walk(c *flowClass, v flowValue) {
	key := walkKey{class: c, value: v}
	if p.seen[key] {
		return
	}
	p.seen[key] = true
	switch v := v.(type) {
	case flowLambda:
		p.moduleArgs(c, v)
		p.walkAll(c, p.evalModuleValue(v.unit, v.fn.Body))
	case flowSet:
		p.moduleSet(c, v)
	case flowPath:
		p.walkAll(c, p.fileValue(v.path))
	case flowTree:
		for _, file := range p.treeFiles(v.dir) {
			p.walkAll(c, p.fileValue(file))
		}
	case flowList:
		for _, item := range v.list.Items {
			p.walkAll(c, p.evalModuleValue(v.unit, item))
		}
	case flowSpace:
		v.space.module = true
		for _, b := range v.space.bound {
			p.walkAll(c, p.evalSource(b.unit, b.source))
		}
		for _, set := range v.space.sets {
			p.walk(c, set)
		}
	case flowUpdate:
		p.walkAll(c, p.eval(v.unit, v.node.Left))
		p.walkAll(c, p.eval(v.unit, v.node.Right))
	}
}

func (p *Project) moduleArgs(c *flowClass, fn flowLambda) {
	p.modular[fn] = true
	param, pattern := fn.fn.Param.(*syntax.PatternParam)
	if !pattern {
		return
	}
	p.addMember(c.args, fn.unit, fn.unit.formals[fn.fn], true)
	for _, formal := range param.Formals {
		if d, ok := fn.unit.f.table.Binder(formal.Name); ok {
			p.bind(d, p.argValue(c, formal.Name.Name))
		}
	}
}

func (p *Project) argValue(c *flowClass, name string) []flowValue {
	switch {
	case name == configFormal || name == optionsFormal:
		return []flowValue{flowSpace{space: c.root}}
	case c.name == classFlake && name == inputsAttr:
		return []flowValue{flowSpace{space: p.inputs}}
	case c.name == classFlake && name == selfFormal:
		return []flowValue{flowSelf{}}
	case c.name == classPerSystem && name == selfPrimed:
		return []flowValue{flowSpace{space: c.root}}
	case c.name == classPerSystem && name == inputsPrimed:
		return []flowValue{flowPrimed{space: p.inputs}}
	}
	return []flowValue{flowSpace{space: p.kid(c.args, name)}}
}

func (p *Project) moduleSet(c *flowClass, v flowSet) {
	p.walkAll(c, p.selectFrom(v, importsAttr))
	options := v.set.names.has(optionsFormal)
	config := v.set.names.has(configFormal)
	if !options && !config {
		p.register(c, c.root, v.unit, v.set, false)
		return
	}
	if options {
		p.registerNamed(c, v, optionsFormal, true)
	}
	if config {
		p.registerNamed(c, v, configFormal, false)
	}
}

func (p *Project) registerNamed(c *flowClass, v flowSet, name string, declared bool) {
	if kid := v.set.kids[name]; kid != nil {
		p.register(c, c.root, v.unit, kid, declared)
	}
	for _, src := range v.set.sources.of(name) {
		for _, value := range p.evalSource(v.unit, src) {
			for _, set := range p.setsOf(value) {
				p.register(c, c.root, set.unit, set.set, declared)
			}
		}
	}
}

func (p *Project) setsOf(v flowValue) []flowSet {
	if set, isSet := v.(flowSet); isSet {
		return []flowSet{set}
	}
	if !p.enter(v) {
		return nil
	}
	defer delete(p.expanding, v)
	switch v := v.(type) {
	case flowUpdate:
		var out []flowSet
		for _, side := range []syntax.Expr{v.node.Left, v.node.Right} {
			for _, value := range p.eval(v.unit, side) {
				out = append(out, p.setsOf(value)...)
			}
		}
		return out
	case flowMapped:
		var out []flowSet
		for _, value := range p.eval(v.unit, v.call.Args[1]) {
			out = append(out, p.setsOf(value)...)
		}
		return out
	}
	return nil
}

func optionType(src flowSource) (syntax.Expr, bool) {
	option, isCall := syntax.Unparenthesized(src.expr).(*syntax.Apply)
	if src.stmt != nil || !isCall || calleeName(syntax.Unparenthesized(option.Fn)) != mkOptionName {
		return nil, false
	}
	set, literal := syntax.Unparenthesized(option.Args[len(option.Args)-1]).(*syntax.AttrSet)
	if !literal {
		return nil, false
	}
	for _, b := range set.Bindings {
		if a, assigned := b.(*syntax.Assign); assigned && lastIdent(a.Path) == "type" {
			return a.Value, true
		}
	}
	return nil, false
}

func (p *Project) optionSubmodule(u *symbolTable, s *space, src flowSource) {
	if t, typed := optionType(src); typed {
		p.submoduleType(u, s, t)
	}
}

func (s *space) moduleBoundary() bool {
	if !s.module {
		return false
	}
	keys := 0
	for at := s; at != nil; at = at.parent {
		for _, twin := range at.twins() {
			for _, b := range twin.bound {
				if t, typed := optionType(b.source); typed {
					return holdsModules(b.unit, t, keys, map[*scope.Def]bool{})
				}
			}
		}
		keys++
	}
	return true
}

func (s *space) twins() []*space {
	if s.parent == nil {
		return []*space{s}
	}
	var out []*space
	for _, up := range s.parent.twins() {
		if kid := up.kids[s.name]; kid != nil {
			out = append(out, kid)
		}
		if wild := up.kids[anyName]; wild != nil && s.name != anyName {
			out = append(out, wild)
		}
	}
	return out
}

func holdsModules(u *symbolTable, t syntax.Expr, keys int, seen map[*scope.Def]bool) bool {
	switch t := syntax.Unparenthesized(t).(type) {
	case *syntax.Ident:
		ref, ok := u.f.table.Of(t)
		if !ok || ref.Kind != scope.Definition {
			return keys == 0 && deferredModuleType(t.Name)
		}
		value, bound := singleValue(ref.Def)
		if !bound || seen[ref.Def] {
			return true
		}
		seen[ref.Def] = true
		return holdsModules(u, value, keys, seen)
	case *syntax.Select:
		return keys == 0 && deferredModuleType(lastIdent(t.Path))
	case *syntax.Apply:
		name := u.f.callName(t.Fn)
		last := t.Args[len(t.Args)-1]
		switch {
		case slices.Contains(wrappingTypes, name):
			return holdsModules(u, last, keys, seen)
		case keys > 0 && slices.Contains(keyedTypes, name):
			return holdsModules(u, last, keys-1, seen)
		case keys == 0:
			return deferredModuleType(name)
		}
	}
	return true
}

func singleValue(d *scope.Def) (syntax.Expr, bool) {
	if len(d.Bindings) != 1 {
		return nil, false
	}
	a, assigned := d.Bindings[0].(*syntax.Assign)
	if !assigned || len(a.Path) != 1 {
		return nil, false
	}
	return a.Value, true
}

func deferredModuleType(name string) bool {
	return name == "deferredModule" || name == "deferredModuleWith"
}

func (p *Project) submoduleType(u *symbolTable, s *space, t syntax.Expr) {
	call, isCall := syntax.Unparenthesized(t).(*syntax.Apply)
	if !isCall || p.tooDeep(s) {
		return
	}
	last := call.Args[len(call.Args)-1]
	switch name := u.f.callName(call.Fn); {
	case name == "submodule":
		p.walkAll(p.subClass(s), p.evalModuleValue(u, last))
	case name == "submoduleWith":
		if set, literal := syntax.Unparenthesized(last).(*syntax.AttrSet); literal {
			for _, b := range set.Bindings {
				if a, assigned := b.(*syntax.Assign); assigned && lastIdent(a.Path) == modulesAttr {
					p.walkAll(p.subClass(s), p.eval(u, a.Value))
				}
			}
		}
	case slices.Contains(keyedTypes, name):
		p.submoduleType(u, p.kid(s, anyName), last)
	case slices.Contains(wrappingTypes, name):
		p.submoduleType(u, s, last)
	}
}

func (p *Project) subClass(s *space) *flowClass {
	if s.sub == nil {
		args := p.newSpace(nil, "", argsRole)
		s.sub = &flowClass{name: classSubmodule, root: s, args: args}
	}
	return s.sub
}

type registration struct {
	into *space
	set  *attrs
}

func (p *Project) register(c *flowClass, s *space, u *symbolTable, set *attrs, declared bool) {
	key := registration{into: s, set: set}
	if p.seen[key] || p.tooDeep(s) {
		return
	}
	p.seen[key] = true
	p.addMember(s, u, set, false)
	for _, kid := range set.dynamicKids() {
		if nested := p.nestedClass(c, append(s.path(), anyName)); nested != nil {
			p.walk(nested, flowSet{unit: u, set: kid})
			for _, value := range kid.directValues() {
				p.walkAll(nested, p.evalModuleValue(u, value))
			}
		}
	}
	for _, name := range set.names.sorted() {
		s.names[name] = true
		kid := p.kid(s, name)
		kid.declared = kid.declared || declared
		if nested := p.nestedClass(c, kid.path()); nested != nil {
			if own := set.kids[name]; own != nil {
				p.walk(nested, flowSet{unit: u, set: own})
			}
			for _, src := range set.sources.of(name) {
				p.walkAll(nested, p.evalSource(u, src))
			}
			continue
		}
		if own := set.kids[name]; own != nil {
			p.register(c, kid, u, own, declared)
		}
		for _, src := range set.sources.of(name) {
			kid.bound = append(kid.bound, flowBound{unit: u, source: src})
			if declared {
				p.optionSubmodule(u, kid, src)
			}
			for _, value := range p.evalSource(u, src) {
				for _, bound := range p.setsOf(value) {
					p.register(c, kid, bound.unit, bound.set, declared)
				}
			}
		}
	}
}

func (p *Project) nestedClass(c *flowClass, path []string) *flowClass {
	switch c.name {
	case classFlake:
		switch {
		case len(path) == 1 && path[0] == classPerSystem:
			return p.class(classPerSystem)
		case len(path) == 4 && path[0] == classFlake && path[1] == modulesAttr:
			return p.class(path[2])
		case len(path) == 3 && path[0] == classFlake:
			for _, o := range moduleOutputs {
				if o.name == path[1] && !o.single {
					return p.class(o.class)
				}
			}
		case len(path) == 2 && path[0] == classFlake:
			for _, o := range moduleOutputs {
				if o.name == path[1] && o.single {
					return p.class(o.class)
				}
			}
		}
	case classNixos, classDarwin, classDroid:
		switch {
		case len(path) == 3 && path[0] == homeManager && path[1] == usersAttr:
			return p.class(classHome)
		case len(path) == 2 && path[0] == homeManager && path[1] == sharedAttr:
			return p.class(classHome)
		}
	}
	return nil
}
