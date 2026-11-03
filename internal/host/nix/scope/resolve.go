package scope

import (
	"slices"

	"github.com/stubbedev/shell-lsp/internal/host/nix/builtins"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

type resolver struct {
	table      *Table
	shadow     map[string]*Def
	defs       syntax.Chunks[Def]
	outers     []*Def
	bindings   syntax.Chunks[syntax.Binding]
	binders    syntax.Chunks[syntax.Node]
	scopes     syntax.Chunks[Scope]
	entries    syntax.Chunks[entry]
	sets       syntax.Chunks[setNode]
	scope      *Scope
	with       *syntax.With
	withIndex  int
	duplicates []pendingDuplicate
}

func (r *resolver) lookup(name string, inherit bool) Ref {
	if name == CurPos && !inherit {
		global, _ := builtins.Find(CurPos)
		return Ref{Kind: Global, Builtin: global}
	}
	if top := r.shadow[name]; top != nil {
		return Ref{Kind: Definition, Def: top}
	}
	if b, ok := builtins.Find(name); ok && b.Global && name != CurPos {
		return Ref{Kind: Global, Builtin: b}
	}
	if r.with != nil {
		return Ref{Kind: Dynamic, With: r.with}
	}
	return Ref{Kind: Undefined}
}

func (r *resolver) resolve(slot int, ref Ref) {
	r.table.refs[slot] = r.packed(ref)
}

func (r *resolver) packed(ref Ref) slotRef {
	switch ref.Kind {
	case Definition:
		return packRef(Definition, int(ref.Def.ID))
	case Global:
		return packRef(Global, builtins.IndexOf(ref.Builtin))
	case Dynamic:
		return packRef(Dynamic, r.withIndex)
	case Undefined:
	}
	return packRef(Undefined, 0)
}

func (r *resolver) ident(id *syntax.Ident) {
	ref := r.lookup(id.Name, false)
	r.resolve(id.Slot(), ref)
	r.table.uses = append(r.table.uses, Use{Node: id, Name: id.Name, Ref: ref})
}

func (r *resolver) inherit(attr syntax.Attr, name string) {
	ref := r.lookup(name, true)
	r.resolve(attr.Slot(), ref)
	r.table.uses = append(r.table.uses, Use{Node: attr, Name: name, Ref: ref, Inherit: true})
}

func (r *resolver) open(kind ScopeKind, spans ...syntax.Span) *Scope {
	s := r.newScope(kind, r.scope)
	r.table.scopes = append(r.table.scopes, s)
	for _, span := range spans {
		r.scope.regions = append(r.scope.regions, region{span: span, scope: s})
	}
	r.scope = s
	return s
}

func (r *resolver) close(s *Scope) {
	for _, d := range s.Defs {
		r.shadow[d.Name] = r.outers[d.ID]
	}
	r.scope = s.Parent
}

func (r *resolver) declare(s *Scope, name string, kind DefKind) *Def {
	top := r.shadow[name]
	if top != nil && top.Scope == s {
		return top
	}
	d := r.defs.New()
	*d = Def{ID: int32(len(r.table.defs)), Name: name, Kind: kind, Scope: s, Implicit: slices.Contains(evaluatorReads, name)} //nolint:gosec // counts the definitions of one file, which the transport caps well below 2^31
	r.outers = append(r.outers, top)
	s.Defs = append(s.Defs, d)
	r.table.defs = append(r.table.defs, d)
	r.shadow[name] = d
	return d
}

func (r *resolver) binder(d *Def, node syntax.Node) {
	d.Binders = r.addBinder(d.Binders, node)
	switch node := node.(type) {
	case *syntax.Ident:
		r.table.binders[node.Slot()] = packDef(d)
		r.resolve(node.Slot(), Ref{Kind: Definition, Def: d})
	case syntax.Attr:
		r.table.binders[node.Slot()] = packDef(d)
	}
}

func (r *resolver) newScope(kind ScopeKind, parent *Scope) *Scope {
	s := r.scopes.New()
	s.Kind, s.Parent = kind, parent
	return s
}

func (r *resolver) addBinder(binders []syntax.Node, node syntax.Node) []syntax.Node {
	if binders != nil {
		return append(binders, node)
	}
	first := r.binders.Take(1)
	first[0] = node
	return first
}

func (r *resolver) keptBindings(bindings []syntax.Binding) []syntax.Binding {
	kept := r.bindings.Take(len(bindings))
	copy(kept, bindings)
	return kept
}

func (r *resolver) exprs(list []syntax.Expr) {
	for _, e := range list {
		r.expr(e)
	}
}

func (r *resolver) expr(e syntax.Expr) {
	switch e := e.(type) {
	case *syntax.Ident:
		r.ident(e)
	case *syntax.String:
		r.parts(e.Parts)
	case *syntax.Path:
		r.parts(e.Parts)
	case *syntax.Paren:
		r.expr(e.Inner)
	case *syntax.List:
		r.exprs(e.Items)
	case *syntax.AttrSet:
		r.drain([]*setNode{r.literal(e)})
	case *syntax.Let:
		r.let(e)
	case *syntax.LegacyLet:
		r.legacyLet(e)
	case *syntax.With:
		r.withExpr(e)
	case *syntax.Assert:
		r.expr(e.Cond)
		r.expr(e.Body)
	case *syntax.If:
		r.expr(e.Cond)
		r.expr(e.Then)
		r.expr(e.Else)
	case *syntax.Lambda:
		r.lambda(e)
	case *syntax.Apply:
		r.expr(e.Fn)
		r.exprs(e.Args)
		if fn, direct := syntax.Unparenthesized(e.Fn).(*syntax.Lambda); direct {
			r.supply(fn, e.Args[0])
		}
	case *syntax.Select:
		r.expr(e.Target)
		r.path(e.Path)
		if e.Default != nil {
			r.expr(e.Default)
		}
	case *syntax.HasAttr:
		r.expr(e.Target)
		r.path(e.Path)
	case *syntax.Binary:
		r.expr(e.Left)
		r.expr(e.Right)
	case *syntax.Unary:
		r.expr(e.Operand)
	}
}

func (r *resolver) parts(parts []syntax.Part) {
	for _, part := range parts {
		if in, ok := part.(*syntax.Interpolation); ok {
			r.expr(in.Value)
		}
	}
}

func (r *resolver) path(path syntax.AttrPath) {
	for _, a := range path {
		r.attrName(a)
	}
}

func (r *resolver) attrName(a syntax.Attr) {
	switch a := a.(type) {
	case *syntax.StringAttr:
		r.parts(a.Value.Parts)
	case *syntax.DynamicAttr:
		r.expr(a.Value)
	}
}

func (r *resolver) withExpr(e *syntax.With) {
	r.expr(e.Scope)
	s := r.open(WithScope, e.Body.Span())
	s.With = e
	r.table.owners[e] = s
	r.table.outerWiths[e] = r.with
	outer, outerIndex := r.with, r.withIndex
	r.with, r.withIndex = e, len(r.table.withs)
	r.table.withs = append(r.table.withs, e)
	r.expr(e.Body)
	r.with, r.withIndex = outer, outerIndex
	r.close(s)
}

func (r *resolver) lambda(e *syntax.Lambda) {
	s := r.open(LambdaScope, e.Span())
	r.table.owners[e] = s
	switch p := e.Param.(type) {
	case *syntax.IdentParam:
		r.binder(r.declare(s, p.Name.Name, ArgDef), p.Name)
	case *syntax.PatternParam:
		r.pattern(s, p)
	}
	r.expr(e.Body)
	r.close(s)
}

func (r *resolver) supply(fn *syntax.Lambda, arg syntax.Expr) {
	switch p := fn.Param.(type) {
	case *syntax.IdentParam:
		r.table.supply(p.Name, arg)
	case *syntax.PatternParam:
		if p.Alias != nil {
			r.table.supply(p.Alias, arg)
		}
		set, literal := syntax.Unparenthesized(arg).(*syntax.AttrSet)
		if !literal {
			return
		}
		for _, f := range p.Formals {
			if value, passed := passedValue(set, f.Name.Name); passed {
				r.table.supply(f.Name, value)
			}
		}
	}
}

func passedValue(set *syntax.AttrSet, name string) (syntax.Expr, bool) {
	for _, b := range set.Bindings {
		a, assigned := b.(*syntax.Assign)
		if !assigned || len(a.Path) != 1 {
			continue
		}
		if own, static := syntax.AttrName(a.Path[0]); static && own == name {
			return a.Value, true
		}
	}
	return nil, false
}

func (r *resolver) pattern(s *Scope, p *syntax.PatternParam) {
	if p.Alias != nil && p.AliasFirst {
		r.binder(r.declare(s, p.Alias.Name, AliasDef), p.Alias)
	}
	for _, f := range p.Formals {
		r.binder(r.declare(s, f.Name.Name, FormalDef), f.Name)
	}
	if p.Alias != nil && !p.AliasFirst {
		r.binder(r.declare(s, p.Alias.Name, AliasDef), p.Alias)
	}
	for _, f := range p.Formals {
		if f.Default != nil {
			r.expr(f.Default)
		}
	}
}

func (r *resolver) let(e *syntax.Let) {
	n := r.newSetNode(true)
	n.owner = e
	n.spans = []syntax.Span{e.Span()}
	r.fill(n, e.Bindings)
	r.recursive(n, letSet, e.Body)
}

func (r *resolver) legacyLet(e *syntax.LegacyLet) {
	n := r.literal(e.Set)
	n.rec = true
	n.spans = []syntax.Span{e.Set.Span()}
	r.recursive(n, legacySet, nil)
}

func (r *resolver) drain(pending []*setNode) {
	for len(pending) > 0 {
		n := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if n.rec {
			r.recursive(n, recSet, nil)
			continue
		}
		r.outward(n)
		pending = r.contents(n, pending)
	}
}

func (r *resolver) outward(n *setNode) {
	for _, e := range n.entries {
		if e.inherit != nil && e.inherit.From == nil {
			r.inherit(e.binders[0], e.name)
		}
	}
	for _, x := range n.extras {
		if x.inherited != nil {
			name, _ := syntax.AttrName(x.inherited)
			r.inherit(x.inherited, name)
		}
	}
}

func (r *resolver) contents(n *setNode, pending []*setNode) []*setNode {
	for _, from := range n.froms {
		r.expr(from.Inner)
	}
	for _, e := range n.entries {
		switch {
		case e.set != nil:
			pending = append(pending, e.set)
		case e.value != nil:
			r.expr(e.value)
		}
	}
	for _, x := range n.extras {
		if x.name != nil {
			r.attrName(x.name)
		}
		if x.value != nil {
			r.expr(x.value)
		}
		if x.set != nil {
			pending = append(pending, x.set)
		}
	}
	return pending
}

func (r *resolver) recursive(n *setNode, kind setKind, body syntax.Expr) {
	r.outward(n)
	s := r.open(kind.scope(), n.spans...)
	s.Defs = slices.Grow(s.Defs, len(n.entries))
	if n.owner != nil {
		r.table.owners[n.owner] = s
	}
	for _, stmt := range n.statements {
		start := stmt.Span().Start
		if stmt.From != nil {
			start = stmt.From.Span().End
		}
		s.regions = append(s.regions, region{
			span:  syntax.Span{Start: start, End: stmt.Span().End},
			scope: r.newScope(InheritScope, s.Parent),
		})
	}
	for _, e := range n.entries {
		r.bind(s, kind, e)
	}
	r.drain(r.contents(n, nil))
	if body != nil {
		r.expr(body)
	}
	r.close(s)
}

func (r *resolver) bind(s *Scope, kind setKind, e *entry) {
	d := r.declare(s, e.name, kind.defKind(e))
	d.Implicit = d.Implicit || kind.readsItself(e.name)
	for _, attr := range e.binders {
		r.binder(d, attr)
	}
	d.Bindings = r.keptBindings(slices.Compact(e.bindings))
}
