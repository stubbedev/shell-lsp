package nix

import (
	"slices"

	"github.com/stubbedev/shell-lsp/internal/host/model"
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
	moduleKeys = []string{importsAttr, optionsFormal, configFormal, "_file", "key", "disabledModules", "freeformType"}
	givenArgs  = []string{configFormal, optionsFormal, "lib", "pkgs", "modulesPath", specialArgs, inputsAttr, selfFormal, "system", selfPrimed, inputsPrimed, withSystemName, "moduleWithSystem", "flake-parts-lib", "getSystem", "final"}
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
	dynamic  bool
}

func (s *space) clear() {
	s.names = map[string]bool{}
	s.members, s.bound, s.sets = nil, nil, nil
	s.declared = false
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
	return &space{parent: parent, name: name, role: role, id: p.nextShared, kids: map[string]*space{}, names: map[string]bool{}}
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
		for name := range inputs.names {
			p.inputs.names[name] = true
		}
	}
	for _, src := range set.sources[outputsAttr] {
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
		for _, b := range v.space.bound {
			p.walkAll(c, p.evalSource(b.unit, b.source))
		}
		for _, set := range v.space.sets {
			p.walk(c, set)
		}
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
		return []flowValue{flowSpace{space: p.inputs}}
	}
	return []flowValue{flowSpace{space: p.kid(c.args, name)}}
}

func (p *Project) moduleSet(c *flowClass, v flowSet) {
	p.walkAll(c, p.selectFrom(v, importsAttr))
	_, options := v.set.names[optionsFormal]
	_, config := v.set.names[configFormal]
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
	for _, src := range v.set.sources[name] {
		for _, value := range p.evalSource(v.unit, src) {
			if set, ok := value.(flowSet); ok {
				p.register(c, c.root, set.unit, set.set, declared)
			}
		}
	}
}

type registration struct {
	into *space
	set  *attrs
}

func (p *Project) register(c *flowClass, s *space, u *symbolTable, set *attrs, declared bool) {
	key := registration{into: s, set: set}
	if p.seen[key] {
		return
	}
	p.seen[key] = true
	p.addMember(s, u, set, false)
	for _, kid := range set.dynKids {
		if nested := p.nestedClass(c, append(s.path(), anyName)); nested != nil {
			p.walk(nested, flowSet{unit: u, set: kid})
			for _, value := range kid.direct {
				p.walkAll(nested, p.evalModuleValue(u, value))
			}
		}
	}
	for _, name := range sortedNames(set.names) {
		s.names[name] = true
		kid := p.kid(s, name)
		kid.declared = kid.declared || declared
		if nested := p.nestedClass(c, kid.path()); nested != nil {
			if own := set.kids[name]; own != nil {
				p.walk(nested, flowSet{unit: u, set: own})
			}
			for _, src := range set.sources[name] {
				p.walkAll(nested, p.evalSource(u, src))
			}
			continue
		}
		if own := set.kids[name]; own != nil {
			p.register(c, kid, u, own, declared)
		}
		for _, src := range set.sources[name] {
			kid.bound = append(kid.bound, flowBound{unit: u, source: src})
			for _, value := range p.evalSource(u, src) {
				if bound, ok := value.(flowSet); ok {
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
