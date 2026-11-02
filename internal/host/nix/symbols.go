package nix

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

const letBlockName = "let"

type entry struct {
	scope model.Scope
	kind  model.Kind
}

type attrs struct {
	id      model.Scope
	kind    model.Kind
	parent  *attrs
	names   map[string]entry
	kids    map[string]*attrs
	reads   map[string]bool
	callers []*attrs
	dynamic bool
	escaped bool
	bound   bool
	sources map[string][]flowSource
	dynKids []*attrs
	direct  []syntax.Expr
	lambda  *syntax.Lambda
}

func (a *attrs) closed() bool {
	for ; a != nil; a = a.parent {
		if a.escaped {
			return false
		}
		if a.bound {
			return true
		}
	}
	return false
}

type export struct {
	scope model.Scope
	doc   string
}

type owner struct {
	def *scope.Def
	set *attrs
}

type symbolTable struct {
	f          *file
	ix         *model.Index
	next       model.Scope
	exports    map[int]export
	envScopes  map[*syntax.AttrSet]model.Scope
	owners     []owner
	sets       map[*syntax.AttrSet]*attrs
	values     map[*scope.Def]*attrs
	lambdas    map[*scope.Def]*syntax.Lambda
	formals    map[*syntax.Lambda]*attrs
	entries    map[*scope.Def]entry
	symbolOf   map[syntax.Attr]int
	statements map[syntax.Attr]*syntax.Inherit
	reads      []syntax.Node
	calls      []*syntax.Apply
	consumed   map[*syntax.Ident]bool
	slots      map[model.Scope]flowSlot
	withSlots  map[*syntax.With]model.Scope
	flowCalls  []flowCall
	chains     []flowChain
	trees      []*syntax.Path
	paths      []*syntax.Path
	loaded     map[*syntax.Path]bool
	selected   map[syntax.Expr]bool
	letAlias   map[syntax.Expr]*scope.Def
}

func newSymbolTable(f *file, ix *model.Index) *symbolTable {
	return &symbolTable{
		f:          f,
		ix:         ix,
		exports:    map[int]export{},
		envScopes:  map[*syntax.AttrSet]model.Scope{},
		sets:       map[*syntax.AttrSet]*attrs{},
		values:     map[*scope.Def]*attrs{},
		lambdas:    map[*scope.Def]*syntax.Lambda{},
		formals:    map[*syntax.Lambda]*attrs{},
		entries:    map[*scope.Def]entry{},
		symbolOf:   map[syntax.Attr]int{},
		statements: map[syntax.Attr]*syntax.Inherit{},
		consumed:   map[*syntax.Ident]bool{},
		slots:      map[model.Scope]flowSlot{},
		withSlots:  map[*syntax.With]model.Scope{},
		loaded:     map[*syntax.Path]bool{},
		selected:   map[syntax.Expr]bool{},
		letAlias:   map[syntax.Expr]*scope.Def{},
	}
}

func (b *symbolTable) newScope() model.Scope {
	b.next++
	return b.next
}

func (b *symbolTable) environment(env environment) model.Scope {
	id, ok := b.envScopes[env.set]
	if !ok {
		id = b.newScope()
		b.envScopes[env.set] = id
	}
	for _, v := range vars(env) {
		if !parser.IsName(b.f.src[v.name.Start:v.name.End]) {
			continue
		}
		if _, taken := b.exports[v.name.Start]; !taken {
			b.exports[v.name.Start] = export{scope: id, doc: envDocs[env.source]}
		}
	}
	return id
}

func (b *symbolTable) fresh(parent *attrs, kind model.Kind) *attrs {
	return &attrs{
		id:      b.newScope(),
		kind:    kind,
		parent:  parent,
		names:   map[string]entry{},
		kids:    map[string]*attrs{},
		reads:   map[string]bool{},
		sources: map[string][]flowSource{},
	}
}

func (b *symbolTable) build() {
	b.expr(b.f.tree.Root, model.NoParent)
	for _, stray := range b.f.tree.Stray {
		b.expr(stray, model.NoParent)
	}
	b.link()
	b.flakeFollows()
	b.pathIncludes()
	b.settleChains()
	b.reads, b.calls = nil, nil
	b.ix.Renames = renames{src: b.f.src, table: b.f.table, ix: b.ix, owners: b.owners, unit: b}
}

func (b *symbolTable) expr(root syntax.Node, parent int) {
	syntax.Inspect(root, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.AttrSet:
			b.set(n, b.fresh(nil, model.KindAttribute), parent)
			return false
		case *syntax.Let:
			b.let(n.Span().Start, n.Bindings, parent)
			b.expr(n.Body, parent)
			return false
		case *syntax.LegacyLet:
			b.sets[n.Set] = b.let(n.Span().Start, n.Set.Bindings, parent)
			return false
		case *syntax.Lambda:
			b.lambda(n, parent)
			return false
		case *syntax.Select, *syntax.HasAttr:
			b.reads = append(b.reads, n)
		case *syntax.Apply:
			b.calls = append(b.calls, n)
		case *syntax.Path:
			b.paths = append(b.paths, n)
		}
		return true
	})
}

func (b *symbolTable) add(sym model.Symbol, o owner) int {
	sym.Arity = model.NoArity
	b.ix.Symbols = append(b.ix.Symbols, sym)
	b.owners = append(b.owners, o)
	return len(b.ix.Symbols) - 1
}

func (b *symbolTable) let(start int, bindings []syntax.Binding, parent int) *attrs {
	into := b.fresh(nil, model.KindVariable)
	into.bound = true
	if len(bindings) == 0 {
		return into
	}
	keyword := model.Span{Start: start, End: start + len(letBlockName)}
	block := model.Span{Start: start, End: bindings[len(bindings)-1].Span().End}
	container := b.add(model.Symbol{Name: letBlockName, Kind: model.KindModule, Span: block, Selection: keyword, Scope: into.id, Parent: parent, Passive: true}, owner{set: into})
	b.bindings(bindings, into, container)
	return into
}

func (b *symbolTable) set(n *syntax.AttrSet, into *attrs, parent int) {
	b.sets[n] = into
	b.bindings(n.Bindings, into, parent)
}

func (b *symbolTable) bindings(list []syntax.Binding, into *attrs, parent int) {
	for _, binding := range list {
		switch binding := binding.(type) {
		case *syntax.Assign:
			b.assign(binding, into, parent)
		case *syntax.Inherit:
			b.inherit(binding, into, parent)
		}
	}
}

func (b *symbolTable) assign(a *syntax.Assign, into *attrs, parent int) {
	value := syntax.Unparenthesized(a.Value)
	_, isFunction := value.(*syntax.Lambda)
	cur, at := into, parent
	var leaf syntax.Attr
	var leafName string
	for i, attr := range a.Path {
		name, ok := syntax.AttrName(attr)
		if !ok {
			b.expr(attr, at)
			cur.dynamic = true
			kid := b.fresh(nil, model.KindAttribute)
			cur.dynKids = append(cur.dynKids, kid)
			cur, leaf = kid, nil
			continue
		}
		last := i == len(a.Path)-1
		kind := cur.kind
		if last && isFunction {
			kind = model.KindFunction
		}
		if i, bound := b.bind(cur, attr, name, a, kind, at); bound {
			at = i
		}
		if last {
			leaf, leafName = attr, name
			continue
		}
		cur = b.child(cur, attr, name)
	}
	switch v := value.(type) {
	case *syntax.AttrSet:
		target := cur
		if leaf != nil {
			target = b.child(cur, leaf, leafName)
		}
		b.set(v, target, at)
	case *syntax.Lambda:
		if def, ok := b.f.table.Binder(leaf); ok {
			b.lambdas[def] = v
		}
		b.lambda(v, at)
		cur.flowsFrom(leafName, leaf, a.Value)
	default:
		b.expr(a.Value, at)
		cur.flowsFrom(leafName, leaf, a.Value)
		b.aliased(into, a, leaf, value)
	}
	b.moduleList(a, value)
}

func (b *symbolTable) child(of *attrs, attr syntax.Attr, name string) *attrs {
	kid, ok := of.kids[name]
	if !ok {
		kid = b.fresh(of, model.KindAttribute)
		of.kids[name] = kid
	}
	if def, ok := b.f.table.Binder(attr); ok {
		b.values[def] = kid
	}
	return kid
}

func nameSpan(a syntax.Attr) model.Span {
	if _, span, ok := syntax.StaticName(a); ok {
		return span
	}
	return a.Span()
}

func (b *symbolTable) bind(into *attrs, attr syntax.Attr, name string, stmt syntax.Binding, kind model.Kind, parent int) (int, bool) {
	spelled, selection, static := syntax.StaticName(attr)
	if !static || name == "" || spelled != name {
		return model.NoSymbol, false
	}
	exported, isExported := b.exports[selection.Start]
	e, known := into.names[name]
	if !known {
		e = entry{scope: into.id, kind: kind}
		if isExported {
			e = entry{scope: exported.scope, kind: model.KindVariable}
		}
		into.names[name] = e
	}
	def, _ := b.f.table.Binder(attr)
	if def != nil {
		b.entries[def] = e
	}
	_, isInherit := stmt.(*syntax.Inherit)
	i := b.add(model.Symbol{
		Name: name, Kind: e.kind, Span: stmt.Span(), Selection: selection,
		Scope: e.scope, Parent: parent, Doc: exported.doc, Exported: isExported, Listed: true,
		Detail: b.f.detail(binding{name: selection, span: stmt.Span(), inherit: isInherit}),
	}, owner{def: def, set: into})
	b.symbolOf[attr] = i
	return i, true
}

func (b *symbolTable) inherit(stmt *syntax.Inherit, into *attrs, parent int) {
	if stmt.From != nil {
		b.expr(stmt.From, parent)
		b.reads = append(b.reads, stmt)
	}
	for _, attr := range stmt.Attrs {
		b.statements[attr] = stmt
		name, ok := syntax.AttrName(attr)
		if !ok {
			b.expr(attr, parent)
			into.dynamic = true
			continue
		}
		into.sources[name] = append(into.sources[name], flowSource{stmt: stmt, attr: attr})
		if i, bound := b.bind(into, attr, name, stmt, into.kind, parent); bound {
			b.ix.Symbols[i].Rewrite = rebound(b.f.src, stmt, attr, name)
		}
	}
}

func (b *symbolTable) lambda(l *syntax.Lambda, parent int) {
	into := b.fresh(nil, model.KindParameter)
	into.lambda = l
	b.formals[l] = into
	switch p := l.Param.(type) {
	case *syntax.IdentParam:
		b.parameter(into, p.Name, p.Span(), parent)
		into.names[p.Name.Name] = entry{scope: into.id, kind: model.KindParameter}
	case *syntax.PatternParam:
		if p.Alias != nil && p.AliasFirst {
			b.alias(into, p.Alias, parent)
		}
		for _, formal := range p.Formals {
			b.parameter(into, formal.Name, formal.Span(), parent)
			into.names[formal.Name.Name] = entry{scope: into.id, kind: model.KindParameter}
		}
		if p.Alias != nil && !p.AliasFirst {
			b.alias(into, p.Alias, parent)
		}
		for _, formal := range p.Formals {
			if formal.Default != nil {
				b.expr(formal.Default, parent)
			}
		}
	}
	b.expr(l.Body, parent)
}

func (b *symbolTable) alias(into *attrs, name *syntax.Ident, parent int) {
	if def := b.parameter(into, name, name.Span(), parent); def != nil {
		b.values[def] = into
	}
}

func (b *symbolTable) parameter(into *attrs, name *syntax.Ident, span model.Span, parent int) *scope.Def {
	if name.Name == "" {
		return nil
	}
	def, _ := b.f.table.Binder(name)
	e := entry{scope: into.id, kind: model.KindParameter}
	if def != nil {
		b.entries[def] = e
	}
	detail := b.f.src[span.Start:span.End]
	if strings.Contains(detail, "\n") {
		detail = name.Name
	}
	b.add(model.Symbol{Name: name.Name, Kind: e.kind, Span: span, Selection: name.Span(), Scope: e.scope, Parent: parent, Detail: detail}, owner{def: def, set: into})
	return def
}

func (b *symbolTable) ref(name string, e entry, span model.Span, rewrite *model.Rewrite) {
	b.ix.Refs = append(b.ix.Refs, model.Ref{Name: name, Kind: e.kind, Span: span, Scope: e.scope, Args: model.NoArgs, Owner: model.NoSymbol, Rewrite: rewrite})
}

func (b *symbolTable) staticSet(e syntax.Expr) *attrs {
	switch e := syntax.Unparenthesized(e).(type) {
	case *syntax.Ident:
		if ref, ok := b.f.table.Of(e); ok && ref.Kind == scope.Definition {
			return b.values[ref.Def]
		}
	case *syntax.AttrSet:
		return b.sets[e]
	}
	return nil
}

func (b *symbolTable) consume(e syntax.Expr) *attrs {
	if id, ok := syntax.Unparenthesized(e).(*syntax.Ident); ok {
		b.consumed[id] = true
	}
	return b.staticSet(e)
}

func (b *symbolTable) link() {
	for _, read := range b.reads {
		switch read := read.(type) {
		case *syntax.Select:
			b.selected[syntax.Unparenthesized(read.Target)] = true
			b.follow(read.Target, read, read.Path, true)
		case *syntax.HasAttr:
			b.selected[syntax.Unparenthesized(read.Target)] = true
			b.follow(read.Target, nil, read.Path, false)
		case *syntax.Inherit:
			b.selected[syntax.Unparenthesized(read.From.Inner)] = true
			b.inheritedFrom(read)
		}
	}
	for _, u := range b.f.table.Uses() {
		if u.Ref.Kind == scope.Definition {
			b.lexical(u)
		}
		if u.Ref.Kind == scope.Dynamic {
			b.throughWith(u)
		}
	}
	for _, call := range b.calls {
		b.call(call)
	}
}

func (b *symbolTable) follow(target syntax.Expr, read syntax.Expr, path syntax.AttrPath, value bool) {
	set := b.consume(target)
	open := b.openable(target)
	names := make([]string, 0, len(path))
	for _, attr := range path {
		name, ok := syntax.AttrName(attr)
		if !ok {
			if set != nil {
				set.escaped = true
			}
			if open {
				b.chains = append(b.chains, flowChain{target: target, path: slices.Clip(names), dynamic: true})
			}
			return
		}
		if set != nil {
			set.reads[name] = true
			if e, known := set.names[name]; known {
				b.ref(name, e, nameSpan(attr), nil)
				set = set.kids[name]
				names = append(names, name)
				continue
			}
			set = nil
		}
		if !open {
			return
		}
		b.openRead(target, slices.Clip(names), attr, name, nil)
		names = append(names, name)
	}
	if value && set != nil {
		set.escaped = true
	}
	if value && open {
		b.chains = append(b.chains, flowChain{target: target, path: names, read: read})
	}
}

func (b *symbolTable) inheritedFrom(stmt *syntax.Inherit) {
	set := b.consume(stmt.From.Inner)
	if set == nil {
		b.inheritedOpen(stmt)
		return
	}
	for _, attr := range stmt.Attrs {
		name, ok := syntax.AttrName(attr)
		if !ok {
			set.escaped = true
			continue
		}
		set.reads[name] = true
		e, known := set.names[name]
		if !known {
			continue
		}
		b.ref(name, e, nameSpan(attr), selected(b.f.src, stmt, attr))
		b.passive(attr)
		if kid := set.kids[name]; kid != nil {
			kid.escaped = true
		}
	}
}

func (b *symbolTable) passive(attr syntax.Attr) {
	if i, ok := b.symbolOf[attr]; ok {
		b.ix.Symbols[i].Passive = true
	}
}

func (b *symbolTable) readUse(u scope.Use, e entry) {
	attr, inherited := u.Node.(syntax.Attr)
	if !inherited {
		b.ref(u.Name, e, u.Node.Span(), nil)
		return
	}
	b.passive(attr)
	b.ref(u.Name, e, nameSpan(attr), kept(b.f.src, b.statements[attr], attr))
}

func (b *symbolTable) lexical(u scope.Use) {
	if id, ok := u.Node.(*syntax.Ident); !ok || !b.consumed[id] {
		if set := b.values[u.Ref.Def]; set != nil {
			set.escaped = true
		}
	}
	if e, ok := b.entries[u.Ref.Def]; ok {
		b.readUse(u, e)
	}
}

func (b *symbolTable) throughWith(u scope.Use) {
	withs := b.f.table.Withs(u.Ref.With)
	for i, with := range withs {
		set := b.staticSet(with.Scope)
		if set == nil {
			b.readUse(u, entry{scope: b.withSlot(withs[i:]), kind: model.KindAttribute})
			return
		}
		set.reads[u.Name] = true
		if e, known := set.names[u.Name]; known {
			b.readUse(u, e)
			return
		}
		if set.dynamic {
			return
		}
	}
}

func (b *symbolTable) call(call *syntax.Apply) {
	b.classify(call)
	fn, ok := syntax.Unparenthesized(call.Fn).(*syntax.Ident)
	if !ok {
		return
	}
	ref, ok := b.f.table.Of(fn)
	if !ok || ref.Kind != scope.Definition {
		return
	}
	lambda := b.lambdas[ref.Def]
	for _, arg := range call.Args {
		if lambda == nil {
			return
		}
		if set, ok := syntax.Unparenthesized(arg).(*syntax.AttrSet); ok && !set.Rec {
			if _, pattern := lambda.Param.(*syntax.PatternParam); pattern {
				b.keys(set, b.formals[lambda])
			}
		}
		lambda, _ = syntax.Unparenthesized(lambda.Body).(*syntax.Lambda)
	}
}

func (b *symbolTable) keys(set *syntax.AttrSet, formals *attrs) {
	formals.callers = append(formals.callers, b.sets[set])
	b.passedKeys(set, func(name string) (entry, bool) {
		e, known := formals.names[name]
		return e, known
	})
}

func (b *symbolTable) passedKeys(set *syntax.AttrSet, formal func(name string) (entry, bool)) {
	key := func(attr syntax.Attr, rewrite func(name string) *model.Rewrite) {
		name, ok := syntax.AttrName(attr)
		if !ok {
			return
		}
		if e, known := formal(name); known {
			b.ref(name, e, nameSpan(attr), rewrite(name))
			b.passive(attr)
		}
	}
	for _, binding := range set.Bindings {
		switch binding := binding.(type) {
		case *syntax.Assign:
			key(binding.Path[0], func(string) *model.Rewrite { return nil })
		case *syntax.Inherit:
			for _, attr := range binding.Attrs {
				key(attr, func(name string) *model.Rewrite { return rebound(b.f.src, binding, attr, name) })
			}
		}
	}
}
