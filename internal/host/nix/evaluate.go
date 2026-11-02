package nix

import (
	"path/filepath"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

type flowValue interface{ flowValue() }

type (
	flowSet struct {
		unit *symbolTable
		set  *attrs
	}
	flowLambda struct {
		unit *symbolTable
		fn   *syntax.Lambda
	}
	flowList struct {
		unit *symbolTable
		list *syntax.List
	}
	flowPath  struct{ path string }
	flowTree  struct{ dir string }
	flowSpace struct{ space *space }
	flowSelf  struct{}
)

func (flowSet) flowValue()    {}
func (flowLambda) flowValue() {}
func (flowList) flowValue()   {}
func (flowPath) flowValue()   {}
func (flowTree) flowValue()   {}
func (flowSpace) flowValue()  {}
func (flowSelf) flowValue()   {}

type flowSelection struct {
	from flowValue
	name string
}

func (p *Project) eval(u *symbolTable, e syntax.Expr) []flowValue {
	e = syntax.Unparenthesized(e)
	if done, ok := p.memo[e]; ok {
		return done
	}
	if p.visiting[e] {
		return nil
	}
	p.visiting[e] = true
	out := p.compute(u, e)
	delete(p.visiting, e)
	p.memo[e] = out
	return out
}

func (p *Project) compute(u *symbolTable, e syntax.Expr) []flowValue {
	switch e := e.(type) {
	case *syntax.Ident:
		ref, ok := u.f.table.Of(e)
		if !ok || ref.Kind != scope.Definition {
			return nil
		}
		return p.evalDef(u, ref.Def)
	case *syntax.Select:
		return p.evalPath(u, e)
	case *syntax.Apply:
		return p.evalApply(u, e)
	case *syntax.AttrSet:
		if set := u.sets[e]; set != nil {
			return []flowValue{flowSet{unit: u, set: set}}
		}
	case *syntax.Lambda:
		return []flowValue{flowLambda{unit: u, fn: e}}
	case *syntax.List:
		return []flowValue{flowList{unit: u, list: e}}
	case *syntax.Path:
		if local(e) {
			return []flowValue{flowPath{path: p.resolve(u, e)}}
		}
	case *syntax.Let:
		return p.eval(u, e.Body)
	case *syntax.With:
		return p.eval(u, e.Body)
	case *syntax.Assert:
		return p.eval(u, e.Body)
	case *syntax.If:
		return union(p.eval(u, e.Then), p.eval(u, e.Else))
	case *syntax.Binary:
		if e.Op == syntax.OpConcat {
			return union(p.eval(u, e.Left), p.eval(u, e.Right))
		}
	}
	return nil
}

func union(a, b []flowValue) []flowValue {
	out := slices.Clone(a)
	for _, v := range b {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

func (p *Project) evalPath(u *symbolTable, e *syntax.Select) []flowValue {
	values := p.eval(u, e.Target)
	for _, attr := range e.Path {
		name, ok := syntax.AttrName(attr)
		if !ok {
			return nil
		}
		values = p.selectAll(values, name)
	}
	return values
}

func (p *Project) evalDef(u *symbolTable, d *scope.Def) []flowValue {
	var out []flowValue
	if set := u.values[d]; set != nil {
		out = append(out, flowSet{unit: u, set: set})
	}
	switch d.Kind {
	case scope.ArgDef, scope.FormalDef, scope.AliasDef:
		return union(out, p.bindings[d])
	}
	if len(out) > 0 {
		return out
	}
	for _, binding := range d.Bindings {
		switch binding := binding.(type) {
		case *syntax.Assign:
			if len(binding.Path) == 1 {
				out = union(out, p.eval(u, binding.Value))
			}
		case *syntax.Inherit:
			out = union(out, p.evalInherit(u, binding, d.Name))
		}
	}
	return out
}

func (p *Project) evalInherit(u *symbolTable, stmt *syntax.Inherit, name string) []flowValue {
	if stmt.From != nil {
		return p.selectAll(p.eval(u, stmt.From.Inner), name)
	}
	for _, attr := range stmt.Attrs {
		if own, ok := syntax.AttrName(attr); !ok || own != name {
			continue
		}
		if ref, ok := u.f.table.Inherited(attr); ok && ref.Kind == scope.Definition {
			return p.evalDef(u, ref.Def)
		}
	}
	return nil
}

func (p *Project) evalSource(u *symbolTable, src flowSource) []flowValue {
	if src.stmt == nil {
		return p.evalModuleValue(u, src.expr)
	}
	name, _ := syntax.AttrName(src.attr)
	return p.evalInherit(u, src.stmt, name)
}

func (p *Project) evalModuleValue(u *symbolTable, e syntax.Expr) []flowValue {
	e = syntax.Unparenthesized(e)
	if update, ok := e.(*syntax.Binary); ok && update.Op == syntax.OpUpdate {
		return union(p.evalModuleValue(u, update.Left), p.evalModuleValue(u, update.Right))
	}
	return p.eval(u, e)
}

func (p *Project) evalApply(u *symbolTable, call *syntax.Apply) []flowValue {
	fn := syntax.Unparenthesized(call.Fn)
	name := calleeName(fn)
	last := call.Args[len(call.Args)-1]
	switch {
	case slices.Contains(transparentCalls, name):
		return p.evalModuleValue(u, last)
	case name == mkMergeName:
		return p.listItems(p.eval(u, last))
	case name == importTreeName:
		return p.treesOf(u, call.Args)
	case name == importName && u.builtinImport(fn):
		return p.applied(p.importValue(u, call.Args[0]), len(call.Args)-1)
	case name == callPackageName:
		return p.applied(p.importValue(u, call.Args[0]), len(call.Args)-1)
	case name == callPackageWithName && len(call.Args) > 1:
		return p.applied(p.importValue(u, call.Args[1]), len(call.Args)-2)
	}
	return p.applied(p.eval(u, fn), len(call.Args))
}

func (p *Project) listItems(values []flowValue) []flowValue {
	var out []flowValue
	for _, v := range values {
		list, ok := v.(flowList)
		if !ok {
			continue
		}
		for _, item := range list.list.Items {
			out = union(out, p.evalModuleValue(list.unit, item))
		}
	}
	return out
}

func (p *Project) treesOf(u *symbolTable, args []syntax.Expr) []flowValue {
	var out []flowValue
	for _, arg := range args {
		for _, v := range p.eval(u, arg) {
			switch v := v.(type) {
			case flowPath:
				out = append(out, flowTree{dir: v.path})
			case flowList:
				for _, item := range v.list.Items {
					out = union(out, p.treesOf(v.unit, []syntax.Expr{item}))
				}
			}
		}
	}
	return out
}

func (p *Project) importValue(u *symbolTable, arg syntax.Expr) []flowValue {
	var out []flowValue
	for _, v := range p.eval(u, arg) {
		if path, ok := v.(flowPath); ok {
			out = union(out, p.fileValue(path.path))
		}
	}
	return out
}

func (p *Project) fileIndex(path string) (int, bool) {
	if i, ok := p.byPath[path]; ok {
		return i, true
	}
	i, ok := p.byPath[filepath.Join(path, DirectoryEntry)]
	return i, ok
}

func (p *Project) fileValue(path string) []flowValue {
	i, ok := p.fileIndex(path)
	if !ok {
		return nil
	}
	u := p.units[i]
	return p.eval(u, u.f.tree.Root)
}

func (p *Project) applied(values []flowValue, args int) []flowValue {
	for range args {
		var next []flowValue
		for _, v := range values {
			if fn, ok := v.(flowLambda); ok {
				next = union(next, p.eval(fn.unit, fn.fn.Body))
			}
		}
		values = next
	}
	return values
}

func (p *Project) selectAll(values []flowValue, name string) []flowValue {
	var out []flowValue
	for _, v := range values {
		out = union(out, p.selectFrom(v, name))
	}
	return out
}

func (p *Project) selectFrom(v flowValue, name string) []flowValue {
	key := flowSelection{from: v, name: name}
	if p.selecting[key] {
		return nil
	}
	p.selecting[key] = true
	defer delete(p.selecting, key)
	switch v := v.(type) {
	case flowSet:
		var out []flowValue
		if kid := v.set.kids[name]; kid != nil {
			out = append(out, flowSet{unit: v.unit, set: kid})
		}
		for _, src := range v.set.sources[name] {
			if src.stmt != nil {
				out = union(out, p.evalSource(v.unit, src))
				continue
			}
			out = union(out, p.eval(v.unit, src.expr))
		}
		return out
	case flowSpace:
		out := []flowValue{flowSpace{space: p.kid(v.space, name)}}
		for _, b := range v.space.bound {
			out = union(out, p.selectAll(p.evalSource(b.unit, b.source), name))
		}
		return out
	case flowSelf:
		return p.selectAll(p.selfValues(), name)
	}
	return nil
}

func (p *Project) selfValues() []flowValue {
	var out []flowValue
	if p.outputs != nil {
		out = p.eval(p.flakeUnit, p.outputs.Body)
	}
	if c, ok := p.classes[classFlake]; ok {
		out = union(out, []flowValue{flowSpace{space: p.kid(c.root, classFlake)}})
	}
	return out
}

func (p *Project) children(v flowValue) []flowValue {
	switch v := v.(type) {
	case flowSet:
		var out []flowValue
		for _, name := range sortedNames(v.set.names) {
			out = union(out, p.selectFrom(v, name))
		}
		return out
	case flowSpace:
		var out []flowValue
		for _, name := range sortedNames(v.space.kids) {
			out = union(out, []flowValue{flowSpace{space: v.space.kids[name]}})
		}
		return out
	}
	return nil
}
