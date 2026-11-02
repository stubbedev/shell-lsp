package nix

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/builtins"
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
	flowPath   struct{ path string }
	flowTree   struct{ dir string }
	flowSpace  struct{ space *space }
	flowSelf   struct{}
	flowPkgs   struct{}
	flowWriter struct{ name string }
	flowScript struct {
		unit *symbolTable
		call *syntax.Apply
	}
	flowUpdate struct {
		unit *symbolTable
		node *syntax.Binary
	}
	flowMapped struct {
		unit *symbolTable
		call *syntax.Apply
	}
	flowPrimed   struct{ space *space }
	flowBuiltin  struct{ name string }
	flowBuiltins struct{}
)

func (flowSet) flowValue()      {}
func (flowLambda) flowValue()   {}
func (flowList) flowValue()     {}
func (flowPath) flowValue()     {}
func (flowTree) flowValue()     {}
func (flowSpace) flowValue()    {}
func (flowSelf) flowValue()     {}
func (flowPkgs) flowValue()     {}
func (flowWriter) flowValue()   {}
func (flowScript) flowValue()   {}
func (flowUpdate) flowValue()   {}
func (flowMapped) flowValue()   {}
func (flowPrimed) flowValue()   {}
func (flowBuiltin) flowValue()  {}
func (flowBuiltins) flowValue() {}

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
	if !p.spend() {
		return nil
	}
	switch e := e.(type) {
	case *syntax.Ident:
		ref, ok := u.f.table.Of(e)
		if !ok {
			return nil
		}
		return p.evalRef(u, ref, e.Name)
	case *syntax.Select:
		return p.evalSelect(u, e)
	case *syntax.String:
		return p.stringPath(u, e)
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
		if loadable(e) {
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
		switch e.Op {
		case syntax.OpConcat:
			return union(p.eval(u, e.Left), p.eval(u, e.Right))
		case syntax.OpUpdate:
			if len(p.eval(u, e.Left)) > 0 && len(p.eval(u, e.Right)) > 0 {
				return []flowValue{flowUpdate{unit: u, node: e}}
			}
		case syntax.OpAdd:
			if suffix, literal := syntax.Unparenthesized(e.Right).(*syntax.String); literal {
				return p.joined(u, e.Left, literalOf(suffix))
			}
		case syntax.OpPipeInto, syntax.OpPipeFrom, syntax.OpImplies, syntax.OpOr, syntax.OpAnd, syntax.OpEq, syntax.OpNe, syntax.OpLt, syntax.OpLe, syntax.OpGt, syntax.OpGe, syntax.OpSub, syntax.OpMul, syntax.OpDiv:
		}
	}
	return nil
}

func (p *Project) evalRef(u *symbolTable, ref scope.Ref, name string) []flowValue {
	switch ref.Kind {
	case scope.Definition:
		return p.evalDef(u, ref.Def)
	case scope.Global:
		return builtinValue(ref.Builtin.Name)
	case scope.Dynamic:
		return union(p.throughWith(u, ref.With, name), writerValue(name))
	case scope.Undefined:
	}
	return writerValue(name)
}

func (p *Project) throughWith(u *symbolTable, innermost *syntax.With, name string) []flowValue {
	for _, with := range u.f.table.Withs(innermost) {
		values := p.eval(u, with.Scope)
		found, decided := []flowValue(nil), len(values) == 0
		for _, v := range values {
			switch v := v.(type) {
			case flowBuiltins:
				found = union(found, p.selectFrom(v, name))
			case flowSet:
				_, named := v.set.names[name]
				if named {
					found = union(found, p.selectFrom(v, name))
				}
				decided = decided || named || v.set.dynamic
			default:
				decided = true
			}
		}
		if decided || len(found) > 0 {
			return found
		}
	}
	return nil
}

func builtinValue(name string) []flowValue {
	if name == builtinsName {
		return []flowValue{flowBuiltins{}}
	}
	if _, known := builtins.Lookup(name); known {
		return []flowValue{flowBuiltin{name: name}}
	}
	return nil
}

func writerValue(name string) []flowValue {
	if knownWriter(name) {
		return []flowValue{flowWriter{name: name}}
	}
	return nil
}

func (p *Project) stringPath(u *symbolTable, e *syntax.String) []flowValue {
	if len(e.Parts) == 0 || len(e.Parts) > 2 {
		return nil
	}
	hole, interpolated := e.Parts[0].(*syntax.Interpolation)
	if !interpolated {
		return nil
	}
	if len(e.Parts) == 1 {
		return p.joined(u, hole.Value, "")
	}
	suffix, literal := e.Parts[1].(*syntax.Literal)
	if !literal {
		return nil
	}
	return p.joined(u, hole.Value, suffix.Text)
}

func (p *Project) joined(u *symbolTable, base syntax.Expr, suffix string) []flowValue {
	var out []flowValue
	for _, v := range p.eval(u, base) {
		if root, ok := p.pathOf(v); ok {
			out = union(out, []flowValue{flowPath{path: filepath.Join(root, suffix)}})
		}
	}
	return out
}

func (p *Project) pathOf(v flowValue) (string, bool) {
	switch v := v.(type) {
	case flowPath:
		return v.path, true
	case flowSelf:
		return p.flakeDir, p.flakeDir != ""
	case flowSpace:
		if v.space.parent != p.inputs || p.flakeUnit == nil {
			return "", false
		}
		lock, ok := p.flakeLockFile()
		if !ok {
			return "", false
		}
		return lock.sourceOf(v.space.name)
	}
	return "", false
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

func (p *Project) evalSelect(u *symbolTable, e *syntax.Select) []flowValue {
	values := p.evalPath(u, e.Target, e.Path)
	if slices.ContainsFunc(values, concrete) {
		return values
	}
	return union(values, writerValue(lastIdent(e.Path)))
}

func concrete(v flowValue) bool {
	switch v.(type) {
	case flowSpace, flowPrimed:
		return false
	}
	return true
}

func (p *Project) evalPath(u *symbolTable, target syntax.Expr, path syntax.AttrPath) []flowValue {
	values := p.eval(u, target)
	for _, attr := range path {
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
		out = union(out, p.bindings[d])
		if d.Name == pkgsFormal {
			out = union(out, []flowValue{flowPkgs{}})
		}
		return union(out, writerValue(d.Name))
	case scope.LetDef, scope.RecDef, scope.InheritDef:
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
		if ref, ok := u.f.table.Inherited(attr); ok {
			return p.evalRef(u, ref, name)
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
	if u.ownFunction(fn) {
		return p.callResult(u, call, p.eval(u, fn))
	}
	switch {
	case slices.Contains(derivationMakers, name):
		return p.derivationValue(u, last)
	case slices.Contains(transparentCalls, name):
		return p.evalModuleValue(u, last)
	case name == mkMergeName:
		return p.listItems(p.eval(u, last))
	case name == importTreeName:
		return p.treesOf(u, call.Args)
	case name == importName && u.builtinImport(fn):
		if len(call.Args) > 1 && p.importsPackages(u, call.Args[0]) {
			return []flowValue{flowPkgs{}}
		}
		return p.applied(p.importValue(u, call.Args[0]), len(call.Args)-1)
	case name == callPackageName:
		return p.applied(p.importValue(u, call.Args[0]), len(call.Args)-1)
	case name == callPackageWithName && len(call.Args) > 1:
		return p.applied(p.importValue(u, call.Args[1]), len(call.Args)-2)
	case name == moduleWithSystemName || slices.Contains(fixers, name):
		return p.applied(p.eval(u, last), 1)
	case name == extendName:
		return p.extended(u, fn, last)
	case name == mapAttrsName && len(call.Args) == 2:
		return []flowValue{flowMapped{unit: u, call: call}}
	case name == attrValuesName:
		var out []flowValue
		for _, v := range p.eval(u, last) {
			out = union(out, p.children(v))
		}
		return out
	case slices.Contains(systemHelpers, name):
		if fn, literal := syntax.Unparenthesized(last).(*syntax.Lambda); literal {
			return p.eval(u, fn.Body)
		}
	case scriptProducer(name):
		return []flowValue{flowScript{unit: u, call: call}}
	}
	return p.callResult(u, call, p.eval(u, fn))
}

func (p *Project) callResult(u *symbolTable, call *syntax.Apply, values []flowValue) []flowValue {
	isWriter := func(v flowValue) bool { _, writer := v.(flowWriter); return writer }
	out := slices.DeleteFunc(slices.Clone(p.applied(values, len(call.Args))), isWriter)
	if slices.ContainsFunc(values, isWriter) || len(p.proveCall(u, call)) > 0 {
		out = union(out, []flowValue{flowScript{unit: u, call: call}})
	}
	return out
}

func (p *Project) derivationValue(u *symbolTable, arg syntax.Expr) []flowValue {
	var out []flowValue
	for _, v := range p.evalModuleValue(u, arg) {
		if fn, isLambda := v.(flowLambda); isLambda {
			out = union(out, p.eval(fn.unit, fn.fn.Body))
			continue
		}
		out = union(out, []flowValue{v})
	}
	return out
}

func (b *symbolTable) ownFunction(fn syntax.Expr) bool {
	id, plain := fn.(*syntax.Ident)
	if !plain {
		return false
	}
	ref, ok := b.f.table.Of(id)
	if !ok || ref.Kind != scope.Definition {
		return false
	}
	return b.lambdas[ref.Def] != nil
}

func (p *Project) importsPackages(u *symbolTable, arg syntax.Expr) bool {
	if path, ok := syntax.Unparenthesized(arg).(*syntax.Path); ok {
		return path.Kind == syntax.SearchPath
	}
	return slices.ContainsFunc(p.eval(u, arg), func(v flowValue) bool {
		s, ok := v.(flowSpace)
		return ok && s.space.parent == p.inputs
	})
}

func (p *Project) extended(u *symbolTable, fn, overlay syntax.Expr) []flowValue {
	sel, ok := fn.(*syntax.Select)
	if !ok {
		return nil
	}
	out := p.evalPath(u, sel.Target, sel.Path[:len(sel.Path)-1])
	for _, v := range p.eval(u, overlay) {
		if lambda, isLambda := v.(flowLambda); isLambda {
			out = union(out, p.applied([]flowValue{lambda}, 2))
		}
	}
	return out
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
		if path, ok := p.pathOf(v); ok {
			out = union(out, p.fileValue(path))
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
		i, ok = p.loadLate(path)
	}
	if !ok {
		return nil
	}
	u := p.units[i]
	return p.eval(u, u.f.tree.Root)
}

func (p *Project) loadLate(path string) (int, bool) {
	if p.peek != nil {
		return p.peekFile(path)
	}
	if p.settled || p.tried[path] {
		return 0, false
	}
	before := len(p.units)
	p.closure([]wantedFile{{path: path, fallback: filepath.Join(path, DirectoryEntry)}})
	if len(p.units) == before {
		return 0, false
	}
	p.changed = true
	return p.fileIndex(path)
}

func (p *Project) peekFile(path string) (int, bool) {
	if p.peeks == maxPeekedFiles || filepath.IsAbs(path) || strings.HasPrefix(path, "~") {
		return 0, false
	}
	p.peeks++
	text, ok := p.peek("./" + filepath.ToSlash(path))
	if !ok {
		return 0, false
	}
	ix := &model.Index{}
	tree := syntax.Parse(text)
	newSymbolTable(&file{src: text, tree: tree, table: scope.Resolve(tree)}, ix).build()
	p.add(path, ix)
	return p.fileIndex(path)
}

func (p *Project) applied(values []flowValue, args int) []flowValue {
	for range args {
		var next []flowValue
		for _, v := range values {
			switch v := v.(type) {
			case flowLambda:
				next = union(next, p.eval(v.unit, v.fn.Body))
			case flowWriter:
				next = union(next, []flowValue{v})
			case flowSpace:
				next = union(next, p.appliedSpace(v.space))
			}
		}
		values = next
	}
	return values
}

func (p *Project) appliedSpace(s *space) []flowValue {
	if p.applying[s] {
		return nil
	}
	p.applying[s] = true
	defer delete(p.applying, s)
	var out []flowValue
	for _, b := range s.bound {
		out = union(out, p.applied(p.evalSource(b.unit, b.source), 1))
	}
	return out
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
	if p.selecting[key] || !p.spend() {
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
		if wild := v.space.kids[anyName]; wild != nil {
			out = append(out, flowSpace{space: wild})
		}
		for _, b := range v.space.bound {
			out = union(out, p.selectAll(p.evalSource(b.unit, b.source), name))
		}
		if v.space.parent == p.inputs {
			out = union(out, p.selectAll(p.foreignOutputs(v.space.name), name))
		}
		return out
	case flowSelf:
		return p.selectAll(p.selfValues(), name)
	case flowPkgs:
		return p.selectAll(p.overlayBodies(), name)
	case flowUpdate:
		right := p.selectAll(p.eval(v.unit, v.node.Right), name)
		if slices.ContainsFunc(right, concrete) {
			return right
		}
		return union(p.selectAll(p.eval(v.unit, v.node.Left), name), right)
	case flowMapped:
		return p.mappedAttr(v, name)
	case flowPrimed:
		return p.primedAttr(v, name)
	case flowBuiltins:
		if strings.HasPrefix(name, builtins.HiddenAliasPrefix) {
			return nil
		}
		return builtinValue(name)
	}
	return nil
}

func (p *Project) mappedAttr(v flowMapped, name string) []flowValue {
	over := p.selectAll(p.eval(v.unit, v.call.Args[1]), name)
	var out []flowValue
	for _, fn := range p.eval(v.unit, v.call.Args[0]) {
		outer, isLambda := fn.(flowLambda)
		if !isLambda {
			continue
		}
		for _, second := range p.eval(outer.unit, outer.fn.Body) {
			inner, curried := second.(flowLambda)
			if !curried {
				continue
			}
			if args, calls := callsParameter(inner); calls {
				out = union(out, p.applied(over, args))
				continue
			}
			out = union(out, p.eval(inner.unit, inner.fn.Body))
		}
	}
	return out
}

func callsParameter(fn flowLambda) (int, bool) {
	param, named := fn.fn.Param.(*syntax.IdentParam)
	call, applied := syntax.Unparenthesized(fn.fn.Body).(*syntax.Apply)
	if !named || !applied {
		return 0, false
	}
	id, plain := syntax.Unparenthesized(call.Fn).(*syntax.Ident)
	if !plain {
		return 0, false
	}
	ref, ok := fn.unit.f.table.Of(id)
	own, bound := fn.unit.f.table.Binder(param.Name)
	if !ok || !bound || ref.Def != own {
		return 0, false
	}
	return len(call.Args), true
}

func (p *Project) primedAttr(v flowPrimed, name string) []flowValue {
	kid := p.kid(v.space, name)
	if v.space == p.inputs {
		return []flowValue{flowPrimed{space: kid}}
	}
	out := []flowValue{flowSpace{space: kid}}
	for _, value := range p.selectAll(p.foreignOutputs(v.space.name), name) {
		out = union(out, p.collapsed(value))
	}
	return out
}

func (p *Project) collapsed(v flowValue) []flowValue {
	set, isSet := v.(flowSet)
	if !isSet || !slices.ContainsFunc(sortedNames(set.set.names), systemName) {
		return []flowValue{v}
	}
	return p.children(v)
}

func systemName(name string) bool {
	return strings.HasSuffix(name, "-linux") || strings.HasSuffix(name, "-darwin")
}

func (p *Project) foreignOutputs(input string) []flowValue {
	if p.flakeUnit == nil {
		return nil
	}
	lock, ok := p.flakeLockFile()
	if !ok {
		return nil
	}
	source, ok := lock.sourceOf(input)
	if !ok {
		return nil
	}
	var out []flowValue
	for _, root := range p.fileValue(filepath.Join(source, FlakeFile)) {
		set, isSet := root.(flowSet)
		if !isSet {
			continue
		}
		for _, src := range set.set.sources[outputsAttr] {
			for _, fn := range p.evalSource(set.unit, src) {
				if lambda, isLambda := fn.(flowLambda); isLambda {
					out = union(out, p.foreignBody(lambda))
				}
			}
		}
	}
	return out
}

func (p *Project) foreignBody(fn flowLambda) []flowValue {
	body := p.eval(fn.unit, fn.fn.Body)
	if param, pattern := fn.fn.Param.(*syntax.PatternParam); pattern {
		for _, formal := range param.Formals {
			if formal.Name.Name != selfFormal {
				continue
			}
			if d, ok := fn.unit.f.table.Binder(formal.Name); ok {
				p.bind(d, body)
			}
		}
	}
	return body
}

func (p *Project) overlayBodies() []flowValue {
	if p.overlayValues != nil {
		return p.overlayValues
	}
	if p.collecting {
		return nil
	}
	p.collecting = true
	defer func() { p.collecting = false }()
	bodies := []flowValue{}
	for i := 0; i < len(p.units); i++ { //nolint:intrange // p.eval can load files through loadLate, which appends to p.units; a range would freeze the length and miss them
		u := p.units[i]
		for _, site := range u.overlays {
			for _, v := range p.eval(u, site) {
				bodies = union(bodies, p.overlayOf(v, map[flowValue]bool{}))
			}
		}
	}
	p.overlayValues = bodies
	return bodies
}

func (p *Project) overlayOf(v flowValue, seen map[flowValue]bool) []flowValue {
	if seen[v] {
		return nil
	}
	seen[v] = true
	switch v := v.(type) {
	case flowLambda:
		var out []flowValue
		for _, second := range p.eval(v.unit, v.fn.Body) {
			inner, curried := second.(flowLambda)
			if !curried {
				continue
			}
			p.bindParam(v, []flowValue{flowPkgs{}})
			p.bindParam(inner, []flowValue{flowPkgs{}})
			out = union(out, p.eval(inner.unit, inner.fn.Body))
		}
		return out
	case flowList:
		var out []flowValue
		for _, item := range v.list.Items {
			for _, value := range p.eval(v.unit, item) {
				out = union(out, p.overlayOf(value, seen))
			}
		}
		return out
	case flowSet, flowUpdate, flowSpace:
		var out []flowValue
		for _, kid := range p.children(v) {
			out = union(out, p.overlayOf(kid, seen))
		}
		return out
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
	if !p.enter(v) {
		return nil
	}
	defer delete(p.expanding, v)
	switch v := v.(type) {
	case flowUpdate:
		out := []flowValue{}
		for _, side := range []syntax.Expr{v.node.Left, v.node.Right} {
			for _, value := range p.eval(v.unit, side) {
				out = union(out, p.children(value))
			}
		}
		return out
	case flowPkgs:
		out := []flowValue{}
		for _, body := range p.overlayBodies() {
			out = union(out, p.children(body))
		}
		return out
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
			for _, b := range v.space.kids[name].bound {
				out = union(out, p.evalSource(b.unit, b.source))
			}
		}
		return out
	}
	return nil
}
