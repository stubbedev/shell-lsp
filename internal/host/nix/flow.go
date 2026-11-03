package nix

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

const (
	callPackageWithName  = "callPackageWith"
	mkMergeName          = "mkMerge"
	importTreeName       = "import-tree"
	withSystemName       = "withSystem"
	moduleWithSystemName = "moduleWithSystem"
	mapAttrsName         = "mapAttrs"
	attrValuesName       = "attrValues"
	extendName           = "extend"
	pkgsFormal           = "pkgs"
	mkOptionName         = "mkOption"
	modulesAttr          = "modules"
	followsAttr          = "follows"
	finalAttrsName       = "finalAttrs"
	inputsAttr           = "inputs"
	FlakeFile            = "flake.nix"
	nixExtension         = ".nix"
)

var (
	transparentCalls = []string{"mkIf", "mkDefault", "mkForce", "mkBefore", "mkAfter", "mkOverride", "mkOrder", "optionalAttrs", "mkPerSystemOption"}
	attrMappers      = []string{mapAttrsName, "mapAttrs'", "mapAttrsToList", "concatMapAttrs"}
	fixers           = []string{"fix", "makeExtensible"}
	derivationMakers = []string{"mkDerivation", "mkShell", "mkShellNoCC"}
	selfApplied      = []string{"mkDerivation", "fix", "makeExtensible"}
	systemHelpers    = []string{"genAttrs", "forAllSystems", "forEachSystem", "eachSystem", "eachDefaultSystem", "perSystem"}
)

type flowSource struct {
	expr syntax.Expr
	stmt *syntax.Inherit
	attr syntax.Attr
}

type flowSlot struct {
	targets []syntax.Expr
	path    []string
	call    *syntax.Apply
	arg     int
}

type scopedSlot struct {
	id   model.Scope
	slot flowSlot
}

func (b *symbolTable) addSlot(id model.Scope, slot flowSlot) {
	b.slots = append(b.slots, scopedSlot{id: id, slot: slot})
}

func (b *symbolTable) slot(id model.Scope) (flowSlot, bool) {
	i, found := slices.BinarySearchFunc(b.slots, id, func(s scopedSlot, target model.Scope) int { return cmp.Compare(s.id, target) })
	if !found {
		return flowSlot{}, false
	}
	return b.slots[i].slot, true
}

func (b *symbolTable) oneTarget(target syntax.Expr) []syntax.Expr {
	out := b.oneTargets.Take(1)
	out[0] = target
	return out
}

type flowChain struct {
	target  syntax.Expr
	path    []string
	read    syntax.Expr
	dynamic bool
}

type callKind uint8

const (
	callImport callKind = iota
	callAuto
	callLocal
	callMkFlake
	callRoot
	callMapAttrs
	callWithSystem
	callFix
)

type flowCall struct {
	kind   callKind
	call   *syntax.Apply
	path   int
	class  string
	closed bool
}

func (a *attrs) flowsFrom(name string, leaf syntax.Attr, value syntax.Expr) {
	if leaf == nil {
		rare := a.rareOnes()
		rare.direct = append(rare.direct, value)
		return
	}
	a.source(name, flowSource{expr: value})
}

func (b *symbolTable) openable(target syntax.Expr) bool {
	id, plain := syntax.Unparenthesized(target).(*syntax.Ident)
	if !plain {
		return true
	}
	ref, ok := b.f.table.Of(id)
	return ok && ref.Kind == scope.Definition
}

func (b *symbolTable) openRead(target syntax.Expr, path []string, attr syntax.Attr, name string, rewrite pendingRewrite) {
	id := b.newScope()
	b.addSlot(id, flowSlot{targets: b.oneTarget(target), path: path})
	b.ref(name, entry{scope: id, kind: model.KindAttribute}, nameSpan(attr), rewrite)
}

func (b *symbolTable) withSlot(withs []*syntax.With) model.Scope {
	if id, done := b.withSlots[withs[0]]; done {
		return id
	}
	id := b.newScope()
	targets := make([]syntax.Expr, len(withs))
	for i, with := range withs {
		targets[i] = with.Scope
		if b.openable(with.Scope) {
			b.chains = append(b.chains, flowChain{target: with.Scope})
		}
	}
	b.addSlot(id, flowSlot{targets: targets})
	b.withSlots[withs[0]] = id
	return id
}

func (b *symbolTable) inheritedOpen(stmt *syntax.Inherit) {
	target := stmt.From.Inner
	if !b.openable(target) {
		return
	}
	for _, attr := range stmt.Attrs {
		name, ok := syntax.AttrName(attr)
		if !ok {
			b.chains = append(b.chains, flowChain{target: target, dynamic: true})
			continue
		}
		b.openRead(target, nil, attr, name, pendingRewrite{form: selectedForm, stmt: stmt, attr: attr})
		b.passive(attr)
	}
}

func (b *symbolTable) openKeys(arg syntax.Expr, call *syntax.Apply, at int) {
	set, literal := syntax.Unparenthesized(arg).(*syntax.AttrSet)
	if !literal || set.Rec {
		return
	}
	id := b.newScope()
	b.addSlot(id, flowSlot{call: call, arg: at})
	b.passedKeys(set, func(string) (entry, bool) { return entry{scope: id, kind: model.KindAttribute}, true })
}

func (b *symbolTable) classify(call *syntax.Apply) {
	fn := syntax.Unparenthesized(call.Fn)
	name := b.f.libraryName(fn)
	switch {
	case name == importName && b.builtinImport(fn):
		b.importing(callImport, call, 0)
	case name == callPackageName:
		b.importing(callAuto, call, 0)
	case name == callPackageWithName:
		b.importing(callAuto, call, 1)
	case name == mkFlakeName:
		b.flowCalls = append(b.flowCalls, flowCall{kind: callMkFlake, call: call})
	case name == importTreeName:
		b.markTrees(call.Args)
	case rootClasses[name] != "":
		b.flowCalls = append(b.flowCalls, flowCall{kind: callRoot, call: call, class: rootClasses[name]})
	case slices.Contains(attrMappers, name):
		b.flowCalls = append(b.flowCalls, flowCall{kind: callMapAttrs, call: call})
	case name == withSystemName:
		b.flowCalls = append(b.flowCalls, flowCall{kind: callWithSystem, call: call, path: 1})
	case name == moduleWithSystemName:
		b.flowCalls = append(b.flowCalls, flowCall{kind: callWithSystem, call: call})
	case slices.Contains(fixers, name):
		b.flowCalls = append(b.flowCalls, flowCall{kind: callFix, call: call})
	default:
		b.localCall(fn, call)
	}
}

func isGlobalBuiltins(table *scope.Table, e syntax.Expr) bool {
	id, plain := syntax.Unparenthesized(e).(*syntax.Ident)
	if !plain || id.Name != builtinsName {
		return false
	}
	ref, ok := table.Of(id)
	return ok && ref.Kind == scope.Global
}

func (b *symbolTable) builtinImport(fn syntax.Expr) bool {
	switch fn := fn.(type) {
	case *syntax.Ident:
		ref, ok := b.f.table.Of(fn)
		return ok && ref.Kind == scope.Global
	case *syntax.Select:
		return isGlobalBuiltins(b.f.table, fn.Target)
	}
	return false
}

func (b *symbolTable) importing(kind callKind, call *syntax.Apply, at int) {
	if at >= len(call.Args) {
		return
	}
	b.markStatic(call.Args[at])
	b.flowCalls = append(b.flowCalls, flowCall{kind: kind, call: call, path: at})
	if at+1 < len(call.Args) {
		b.openKeys(call.Args[at+1], call, at+1)
	}
}

func (b *symbolTable) localCall(fn syntax.Expr, call *syntax.Apply) {
	switch fn := fn.(type) {
	case *syntax.Lambda:
	case *syntax.Ident:
		ref, ok := b.f.table.Of(fn)
		if !ok || ref.Kind != scope.Definition {
			return
		}
		if b.lambdas[ref.Def] == nil && b.importedDef(ref.Def) {
			b.openKeys(call.Args[0], call, 0)
		}
	case *syntax.Select:
		switch target := syntax.Unparenthesized(fn.Target).(type) {
		case *syntax.Apply:
		case *syntax.Ident:
			ref, ok := b.f.table.Of(target)
			if !ok || ref.Kind != scope.Definition || ref.Def.Kind != scope.LetDef {
				return
			}
		default:
			return
		}
	default:
		return
	}
	b.flowCalls = append(b.flowCalls, flowCall{kind: callLocal, call: call})
}

func (b *symbolTable) importedDef(d *scope.Def) bool {
	bound, ok := singleValue(d)
	if !ok {
		return false
	}
	value, applied := syntax.Unparenthesized(bound).(*syntax.Apply)
	return applied && b.f.callName(value.Fn) == importName
}

func (b *symbolTable) markTrees(args []syntax.Expr) {
	for _, arg := range args {
		switch arg := syntax.Unparenthesized(arg).(type) {
		case *syntax.Path:
			b.trees = append(b.trees, arg)
		case *syntax.List:
			b.markTrees(arg.Items)
		case *syntax.Ident:
			if p := b.staticPath(arg); p != nil {
				b.trees = append(b.trees, p)
			}
		}
	}
}

func (b *symbolTable) staticPath(id *syntax.Ident) *syntax.Path {
	ref, ok := b.f.table.Of(id)
	if !ok || ref.Kind != scope.Definition {
		return nil
	}
	value, bound := singleValue(ref.Def)
	if !bound {
		return nil
	}
	p, _ := syntax.Unparenthesized(value).(*syntax.Path)
	return p
}

func (b *symbolTable) markStatic(e syntax.Expr) {
	switch e := syntax.Unparenthesized(e).(type) {
	case *syntax.Path:
		b.loaded[e] = true
	case *syntax.Ident:
		if p := b.staticPath(e); p != nil {
			b.loaded[p] = true
		}
	}
}

func (b *symbolTable) moduleList(a *syntax.Assign, value syntax.Expr) {
	if name := lastIdent(a.Path); name == importsAttr || name == modulesAttr {
		b.markItems(value)
	}
}

func (b *symbolTable) markItems(value syntax.Expr) {
	switch value := syntax.Unparenthesized(value).(type) {
	case *syntax.List:
		for _, item := range value.Items {
			b.markStatic(item)
		}
	case *syntax.Binary:
		if value.Op == syntax.OpConcat {
			b.markItems(value.Left)
			b.markItems(value.Right)
		}
	}
}

func (b *symbolTable) aliased(into *attrs, a *syntax.Assign, leaf syntax.Attr, value syntax.Expr) {
	if !into.bound || len(a.Path) != 1 {
		return
	}
	if def, ok := b.f.table.Binder(leaf); ok {
		b.letAlias[value] = def
	}
}

func (b *symbolTable) consumedAlias(e syntax.Expr) bool {
	def, bound := b.letAlias[syntax.Unparenthesized(e)]
	if !bound {
		return false
	}
	return !slices.ContainsFunc(def.Uses(), func(u scope.Use) bool {
		id, plain := u.Node.(*syntax.Ident)
		return !plain || !b.consumed[id.Slot()]
	})
}

func (b *symbolTable) settleChains() {
	b.chains = slices.DeleteFunc(b.chains, func(c flowChain) bool { return c.read != nil && b.consumedAlias(c.read) })
	for i := range b.flowCalls {
		call := b.flowCalls[i].call
		b.flowCalls[i].closed = b.selected[call] || b.consumedAlias(call)
	}
	clear(b.selected)
	clear(b.letAlias)
	clear(b.loaded)
}

func (b *symbolTable) pathIncludes() {
	b.ix.Includes = appendPathIncludes(b.ix.Includes, b.f.src, b.paths, b.loaded)
}

func appendPathIncludes(includes []model.Include, src string, paths []*syntax.Path, loaded map[*syntax.Path]bool) []model.Include {
	slices.SortFunc(paths, func(x, y *syntax.Path) int { return cmp.Compare(x.Span().Start, y.Span().Start) })
	includes = slices.Grow(includes, len(paths))
	for _, p := range paths {
		if !loadable(p) {
			continue
		}
		span := p.Span()
		text := src[span.Start:span.End]
		entry := text + "/" + DirectoryEntry
		isFile := strings.HasSuffix(text, nixExtension)
		candidates := []string{entry}
		if isFile || loaded[p] {
			candidates = []string{text, entry}
		}
		includes = append(includes, model.Include{Span: span, Path: text, Candidates: candidates, Optional: true, Probe: !isFile && !loaded[p]})
	}
	return includes
}

func (b *symbolTable) flakeFollows() {
	root, isSet := syntax.Unparenthesized(b.f.tree.Root).(*syntax.AttrSet)
	if !isSet || b.sets[root] == nil {
		return
	}
	if inputs := b.sets[root].kids[inputsAttr]; inputs != nil {
		b.followsOf(inputs, inputs)
	}
}

func (b *symbolTable) followsOf(of, inputs *attrs) {
	for _, name := range slices.Sorted(maps.Keys(of.kids)) {
		kid := of.kids[name]
		for _, src := range kid.sources.of(followsAttr) {
			if text, ok := syntax.Unparenthesized(src.expr).(*syntax.String); ok {
				b.followed(text, inputs)
			}
		}
		if nested := kid.kids[inputsAttr]; nested != nil {
			b.followsOf(nested, inputs)
		}
	}
}

func (b *symbolTable) followed(text *syntax.String, inputs *attrs) {
	if len(text.Parts) != 1 {
		return
	}
	literal, plain := text.Parts[0].(*syntax.Literal)
	if !plain {
		return
	}
	name, _, _ := strings.Cut(literal.Text, "/")
	e, known := inputs.names.get(name)
	if !known {
		return
	}
	start := literal.Span().Start
	b.ref(name, e, model.Span{Start: start, End: start + len(name)}, pendingRewrite{})
}
