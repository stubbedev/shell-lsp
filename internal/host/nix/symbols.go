package nix

import (
	"maps"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

const (
	slotsPerFlowSlot = 3
	letBlockName     = "let"
	passAsFile       = "passAsFile"
	makeWrapperArgs  = "makeWrapperArgs"
	overlaysAttr     = "overlays"
)

var wrapperSetFlags = []string{"--set", "--set-default"}

type entry struct {
	scope model.Scope
	kind  model.Kind
}

type attrs struct {
	id      model.Scope
	kind    model.Kind
	parent  *attrs
	names   nameTable[entry]
	kids    map[string]*attrs
	rare    *rareAttrs
	left    int32
	dynamic bool
	bound   bool
	sources nameTable[[]flowSource]
	lambda  *syntax.Lambda
}

type rareAttrs struct {
	reads   nameTable[struct{}]
	callers []*attrs
	dynKids []*attrs
	direct  []syntax.Expr
}

func (a *attrs) rareOnes() *rareAttrs {
	if a.rare == nil {
		a.rare = &rareAttrs{}
	}
	return a.rare
}

func (a *attrs) dynamicKids() []*attrs {
	if a.rare == nil {
		return nil
	}
	return a.rare.dynKids
}

func (a *attrs) directValues() []syntax.Expr {
	if a.rare == nil {
		return nil
	}
	return a.rare.direct
}

func (a *attrs) name(name string, e entry) { a.names.set(name, e) }

func (a *attrs) readsName(name string) bool { return a.rare != nil && a.rare.reads.has(name) }

func (a *attrs) read(name string) {
	reads := &a.rareOnes().reads
	if !reads.has(name) {
		reads.set(name, struct{}{})
	}
}

func (a *attrs) source(name string, src flowSource) {
	a.sources.set(name, append(a.sources.of(name), src))
}

func (a *attrs) leave(offset int) {
	if a.left == 0 {
		a.left = model.Narrow(offset) + 1
	}
}

func (a *attrs) leftAt() (int, bool) {
	for ; a != nil; a = a.parent {
		if a.left > 0 {
			return int(a.left) - 1, true
		}
	}
	return 0, false
}

func (a *attrs) closed() bool {
	for ; a != nil; a = a.parent {
		if a.left > 0 {
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
	attrChunks syntax.Chunks[attrs]
	oneTargets syntax.Chunks[syntax.Expr]
	exports    map[int]export
	envScopes  map[environment]model.Scope
	shellEnvs  map[model.Scope]environment
	sinks      map[*syntax.Lambda]*lambdaSinks
	candidates []*syntax.Apply
	overlays   []syntax.Expr
	owners     []owner
	sets       map[*syntax.AttrSet]*attrs
	values     map[*scope.Def]*attrs
	lambdas    map[*scope.Def]*syntax.Lambda
	formals    map[*syntax.Lambda]*attrs
	entries    []entry
	symbolOf   []int32
	statements []*syntax.Inherit
	reads      []syntax.Node
	calls      []*syntax.Apply
	consumed   []bool
	slots      []scopedSlot
	withSlots  map[*syntax.With]model.Scope
	flowCalls  []flowCall
	chains     []flowChain
	trees      []*syntax.Path
	paths      []*syntax.Path
	loaded     map[*syntax.Path]bool
	selected   map[syntax.Expr]bool
	letAlias   map[syntax.Expr]*scope.Def

	symbolRewrites []pendingRewrite
	refRewrites    []pendingRewrite
}

func newSymbolTable(f *file, ix *model.Index) *symbolTable {
	slots := f.tree.Slots
	return &symbolTable{
		f:          f,
		ix:         ix,
		exports:    map[int]export{},
		envScopes:  map[environment]model.Scope{},
		shellEnvs:  map[model.Scope]environment{},
		sets:       map[*syntax.AttrSet]*attrs{},
		values:     map[*scope.Def]*attrs{},
		lambdas:    map[*scope.Def]*syntax.Lambda{},
		formals:    map[*syntax.Lambda]*attrs{},
		entries:    make([]entry, len(f.table.Defs())),
		symbolOf:   make([]int32, slots),
		statements: make([]*syntax.Inherit, slots),
		consumed:   make([]bool, slots),
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
	id, ok := b.envScopes[env]
	if ok {
		return id
	}
	id = b.newScope()
	b.envScopes[env] = id
	b.shellEnvs[id] = env
	if env.set == nil {
		return id
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
	a := b.attrChunks.New()
	a.id, a.kind, a.parent = b.newScope(), kind, parent
	return a
}

func (b *symbolTable) build() {
	b.ix.Refs = slices.Grow(b.ix.Refs, b.f.tree.Slots)
	b.ix.Symbols = slices.Grow(b.ix.Symbols, b.f.tree.Binders)
	b.owners = slices.Grow(b.owners, b.f.tree.Binders)
	b.slots = slices.Grow(b.slots, b.f.tree.Slots/slotsPerFlowSlot)
	b.expr(b.f.tree.Root, model.NoParent)
	for _, stray := range b.f.tree.Stray {
		b.expr(stray, model.NoParent)
	}
	b.exportExtras()
	b.link()
	b.flakeFollows()
	b.pathIncludes()
	b.settleChains()
	b.sinks = b.f.sinks()
	b.candidates = b.wrapperCandidates()
	b.reads, b.calls = nil, nil
	b.publish()
}

func (b *symbolTable) compact() {
	b.entries, b.symbolOf, b.statements, b.consumed = nil, nil, nil, nil
	b.loaded, b.selected, b.letAlias, b.exports, b.envScopes = nil, nil, nil, nil, nil
	b.paths = nil
	b.attrChunks, b.oneTargets = syntax.Chunks[attrs]{}, syntax.Chunks[syntax.Expr]{}
	b.tighten()
	b.publish()
}

func (b *symbolTable) tighten() {
	b.symbolRewrites, b.refRewrites = parser.Tightened(b.symbolRewrites), parser.Tightened(b.refRewrites)
	b.ix.Symbols = parser.Tightened(b.ix.Symbols)
	b.ix.Refs = parser.Tightened(b.ix.Refs)
	b.owners = parser.Tightened(b.owners)
	b.slots = parser.Tightened(b.slots)
}

func (b *symbolTable) publish() {
	b.ix.Renames = renames{src: b.f.src, table: b.f.table, ix: b.ix, owners: b.owners, unit: b}
}

func (b *symbolTable) exportExtras() {
	for _, id := range slices.Sorted(maps.Keys(b.shellEnvs)) {
		env := b.shellEnvs[id]
		if env.set == nil || env.source != derivationEnv {
			continue
		}
		set := b.sets[env.set]
		if set == nil {
			continue
		}
		for _, a := range env.set.Bindings {
			assign, assigned := a.(*syntax.Assign)
			if !assigned || len(assign.Path) != 1 {
				continue
			}
			list, isList := syntax.Unparenthesized(assign.Value).(*syntax.List)
			if !isList {
				continue
			}
			switch lastIdent(assign.Path) {
			case passAsFile:
				b.passedAsFiles(id, set, list)
			case makeWrapperArgs:
				b.wrapperArguments(id, set, list)
			}
		}
	}
}

func (b *symbolTable) passedAsFiles(id model.Scope, set *attrs, list *syntax.List) {
	for _, item := range list.Items {
		str, isString := syntax.Unparenthesized(item).(*syntax.String)
		if !isString || literalOf(str) == "" {
			continue
		}
		name := literalOf(str)
		selection := str.Content
		for i, s := range b.ix.Symbols {
			if s.Name == name && s.Scope == set.id && !s.Hidden {
				selection = b.ix.Symbols[i].Selection
			}
		}
		b.exportExtra(id, set, name+"Path", selection, "The path of a file that holds `"+name+"`, which `passAsFile` keeps out of the environment.")
	}
}

func (b *symbolTable) wrapperArguments(id model.Scope, set *attrs, list *syntax.List) {
	for i := 0; i+1 < len(list.Items); i++ {
		flag, isString := syntax.Unparenthesized(list.Items[i]).(*syntax.String)
		name, named := syntax.Unparenthesized(list.Items[i+1]).(*syntax.String)
		if !isString || !named || !slices.Contains(wrapperSetFlags, literalOf(flag)) || !parser.IsName(literalOf(name)) {
			continue
		}
		b.exportExtra(id, set, literalOf(name), name.Content, "Set by `makeWrapperArgs` in the environment of the wrapped program; the phases of this derivation do not see it.")
	}
}

func (b *symbolTable) exportExtra(id model.Scope, set *attrs, name string, selection model.Span, doc string) {
	b.extendShell(id, set.id)
	b.add(model.Symbol{
		Name: name, Kind: model.KindVariable, Span: selection, Selection: selection, Scope: set.id,
		Parent: model.NoParent, Doc: doc, Exported: true, Hidden: true, Passive: true,
		Detail: b.f.src[selection.Start:selection.End],
	}, owner{set: set})
	set.name(name, entry{scope: set.id, kind: model.KindVariable})
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
		case *syntax.Binary:
			if call, piped := n.Piped(); piped {
				b.calls = append(b.calls, call)
			}
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
	into.names.reserve(len(n.Bindings))
	into.sources.reserve(len(n.Bindings))
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
			rare := cur.rareOnes()
			rare.dynKids = append(rare.dynKids, kid)
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
	if overlayPath(a.Path) {
		b.overlays = append(b.overlays, a.Value)
	}
}

func (b *symbolTable) child(of *attrs, attr syntax.Attr, name string) *attrs {
	kid, ok := of.kids[name]
	if !ok {
		kid = b.fresh(of, model.KindAttribute)
		if of.kids == nil {
			of.kids = map[string]*attrs{}
		}
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
	e, known := into.names.get(name)
	if !known {
		e = entry{scope: into.id, kind: kind}
		if isExported {
			e.kind = model.KindVariable
			b.extendShell(exported.scope, into.id)
		}
		into.name(name, e)
	}
	def, _ := b.f.table.Binder(attr)
	if def != nil {
		b.entries[def.ID] = e
	}
	_, isInherit := stmt.(*syntax.Inherit)
	detail, elided := b.f.detail(binding{name: selection, span: stmt.Span(), inherit: isInherit})
	i := b.add(model.Symbol{
		Name: name, Kind: e.kind, Span: stmt.Span(), Selection: selection,
		Scope: e.scope, Parent: parent, Doc: exported.doc, Exported: isExported, Listed: true,
		Detail: detail, Elided: elided,
	}, owner{def: def, set: into})
	b.symbolOf[attr.Slot()] = int32(i + 1) //nolint:gosec // bounded by the source length, which the transport caps well below 2^31
	return i, true
}

func (b *symbolTable) extendShell(shell, set model.Scope) {
	if b.ix.Extends == nil {
		b.ix.Extends = map[model.Scope][]model.Scope{}
	}
	if !slices.Contains(b.ix.Extends[shell], set) {
		b.ix.Extends[shell] = append(b.ix.Extends[shell], set)
	}
}

func (b *symbolTable) inherit(stmt *syntax.Inherit, into *attrs, parent int) {
	if stmt.From != nil {
		b.expr(stmt.From, parent)
		b.reads = append(b.reads, stmt)
	}
	for _, attr := range stmt.Attrs {
		b.statements[attr.Slot()] = stmt
		name, ok := syntax.AttrName(attr)
		if !ok {
			b.expr(attr, parent)
			into.dynamic = true
			continue
		}
		into.source(name, flowSource{stmt: stmt, attr: attr})
		if i, bound := b.bind(into, attr, name, stmt, into.kind, parent); bound {
			b.symbolRewrites = append(b.symbolRewrites, pendingRewrite{at: model.Narrow(i), form: reboundForm, stmt: stmt, attr: attr, name: name})
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
		into.name(p.Name.Name, entry{scope: into.id, kind: model.KindParameter})
	case *syntax.PatternParam:
		if p.Alias != nil && p.AliasFirst {
			b.alias(into, p.Alias, parent)
		}
		for _, formal := range p.Formals {
			b.parameter(into, formal.Name, formal.Span(), parent)
			into.name(formal.Name.Name, entry{scope: into.id, kind: model.KindParameter})
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
		b.entries[def.ID] = e
	}
	detail := b.f.src[span.Start:span.End]
	if strings.Contains(detail, "\n") {
		detail = name.Name
	}
	b.add(model.Symbol{Name: name.Name, Kind: e.kind, Span: span, Selection: name.Span(), Scope: e.scope, Parent: parent, Detail: detail}, owner{def: def, set: into})
	return def
}

func (b *symbolTable) ref(name string, e entry, span model.Span, rewrite pendingRewrite) {
	if rewrite.form != noRewrite {
		rewrite.at = model.Narrow(len(b.ix.Refs))
		b.refRewrites = append(b.refRewrites, rewrite)
	}
	b.ix.Refs = append(b.ix.Refs, model.Ref{Name: name, Kind: e.kind, Span: span, Scope: e.scope, Args: model.NoArgs, Owner: model.NoSymbol})
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
		b.consumed[id.Slot()] = true
	}
	return b.staticSet(e)
}

func (b *symbolTable) link() {
	b.selected = make(map[syntax.Expr]bool, len(b.reads))
	b.chains = slices.Grow(b.chains, len(b.reads))
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
				set.leave(attr.Span().Start)
			}
			if open {
				b.chains = append(b.chains, flowChain{target: target, path: slices.Clip(names), dynamic: true})
			}
			return
		}
		if set != nil {
			set.read(name)
			if e, known := set.names.get(name); known {
				b.ref(name, e, nameSpan(attr), pendingRewrite{})
				set = set.kids[name]
				names = append(names, name)
				continue
			}
			set = nil
		}
		if !open {
			return
		}
		b.openRead(target, slices.Clip(names), attr, name, pendingRewrite{})
		names = append(names, name)
	}
	if value && set != nil {
		set.leave(target.Span().Start)
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
			set.leave(attr.Span().Start)
			continue
		}
		set.read(name)
		e, known := set.names.get(name)
		if !known {
			continue
		}
		b.ref(name, e, nameSpan(attr), pendingRewrite{form: selectedForm, stmt: stmt, attr: attr})
		b.passive(attr)
		if kid := set.kids[name]; kid != nil {
			kid.leave(attr.Span().Start)
		}
	}
}

func (b *symbolTable) passive(attr syntax.Attr) {
	if i := b.symbolOf[attr.Slot()]; i > 0 {
		b.ix.Symbols[i-1].Passive = true
	}
}

func (b *symbolTable) readUse(u scope.Use, e entry) {
	attr, inherited := u.Node.(syntax.Attr)
	if !inherited {
		b.ref(u.Name, e, u.Node.Span(), pendingRewrite{})
		return
	}
	b.passive(attr)
	b.ref(u.Name, e, nameSpan(attr), pendingRewrite{form: keptForm, stmt: b.statements[attr.Slot()], attr: attr})
}

func (b *symbolTable) lexical(u scope.Use) {
	if id, ok := u.Node.(*syntax.Ident); !ok || !b.consumed[id.Slot()] {
		if set := b.values[u.Ref.Def]; set != nil {
			set.leave(u.Node.Span().Start)
		}
	}
	if e := b.entries[u.Ref.Def.ID]; e.scope != model.Scope(0) {
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
		set.read(u.Name)
		if e, known := set.names.get(u.Name); known {
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
	rare := formals.rareOnes()
	rare.callers = append(rare.callers, b.sets[set])
	b.passedKeys(set, func(name string) (entry, bool) {
		return formals.names.get(name)
	})
}

func (b *symbolTable) passedKeys(set *syntax.AttrSet, formal func(name string) (entry, bool)) {
	key := func(attr syntax.Attr, rewrite func(name string) pendingRewrite) {
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
			key(binding.Path[0], func(string) pendingRewrite { return pendingRewrite{} })
		case *syntax.Inherit:
			for _, attr := range binding.Attrs {
				key(attr, func(name string) pendingRewrite {
					return pendingRewrite{form: reboundForm, stmt: binding, attr: attr, name: name}
				})
			}
		}
	}
}
