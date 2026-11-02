package nix

import (
	"fmt"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

const (
	minRepeatedKey = 3
	builtinPrefix  = "__"
)

func (c *checker) selectTarget(n *syntax.Select) {
	p, ok := n.Target.(*syntax.Paren)
	if !ok {
		return
	}
	switch p.Inner.(type) {
	case *syntax.Select:
		if n.Default != nil {
			c.parens[p] = parenRequired
		}
	case *syntax.Int, *syntax.Float, *syntax.URI, *syntax.Path:
		c.parens[p] = parenRequired
	}
}

func (c *checker) binary(b *syntax.Binary) {
	c.bindsTighter(b)
	switch b.Op {
	case syntax.OpEq, syntax.OpNe:
		c.boolComparison(b)
	case syntax.OpConcat:
		c.emptyListConcat(b)
	case syntax.OpUpdate:
		c.recOverride(b)
	default:
	}
}

func (c *checker) bindsTighter(b *syntax.Binary) {
	switch b.Op {
	case syntax.OpOr, syntax.OpAnd, syntax.OpImplies, syntax.OpPipeInto, syntax.OpPipeFrom:
		return
	default:
	}
	c.markTight(b.Left)
	c.markTight(b.Right)
}

func (c *checker) markTight(e syntax.Expr) {
	u, ok := e.(*syntax.Unary)
	if !ok {
		return
	}
	if c.tight == nil {
		c.tight = map[*syntax.Unary]bool{}
	}
	c.tight[u] = true
}

func (c *checker) unary(u *syntax.Unary) {
	c.markTight(u.Operand)
	if p, ok := u.Operand.(*syntax.Paren); ok {
		c.parens[p] = parenPrefixed
	}
	c.boolSimplification(u)
}

func (c *checker) boolComparison(b *syntax.Binary) {
	left, leftLiteral := c.booleanLiteral(b.Left)
	right, rightLiteral := c.booleanLiteral(b.Right)
	if !leftLiteral || !rightLiteral {
		return
	}
	value := falseLiteral
	if (left == right) == (b.Op == syntax.OpEq) {
		value = trueLiteral
	}
	c.report(lint.CodeNixBoolComparison, b.Span(), "this compares two Boolean literals, so its value is already known",
		c.offer("Use `"+value+"`", lint.Safe, textedit.Edit{Start: b.Span().Start, End: b.Span().End, NewText: value}))
}

func (c *checker) boolSimplification(u *syntax.Unary) {
	if u.Op != syntax.OpNot {
		return
	}
	p, ok := u.Operand.(*syntax.Paren)
	if !ok {
		return
	}
	eq, ok := p.Inner.(*syntax.Binary)
	if !ok || eq.Op != syntax.OpEq {
		return
	}
	opener, closer := "", ""
	if c.tight[u] {
		opener, closer = "(", ")"
	}
	span := p.Span()
	c.report(lint.CodeNixBoolSimplification, u.Span(), "`!(a == b)` is what `a != b` says",
		c.offer("Use `!=`", lint.Safe,
			textedit.Edit{Start: u.Span().Start, End: eq.Span().Start, NewText: opener},
			textedit.Edit{Start: eq.OpSpan.Start, End: eq.OpSpan.End, NewText: syntax.OpNe.String()},
			textedit.Edit{Start: eq.Span().End, End: span.End, NewText: closer}))
}

func (c *checker) emptyListConcat(b *syntax.Binary) {
	kept := b.Right
	if !emptyList(b.Left) {
		if !emptyList(b.Right) {
			return
		}
		kept = b.Left
	}
	c.report(lint.CodeNixEmptyListConcat, b.Span(), "joining with an empty list changes nothing",
		c.offer("Remove the empty list", lint.Safe, textedit.Edit{Start: b.Span().Start, End: b.Span().End, NewText: c.textOf(kept.Span())}))
}

func emptyList(e syntax.Expr) bool {
	l, ok := e.(*syntax.List)
	return ok && len(l.Items) == 0
}

func (c *checker) manualInheritFrom(a *syntax.Assign) {
	if len(a.Path) != 1 {
		return
	}
	attr, isIdent := a.Path[0].(*syntax.IdentAttr)
	value, isSelect := a.Value.(*syntax.Select)
	if !isIdent || !isSelect || value.Default != nil || len(value.Path) != 1 {
		return
	}
	last, sameKind := value.Path[0].(*syntax.IdentAttr)
	if !sameKind || last.Name != attr.Name {
		return
	}
	from := c.textOf(value.Target.Span())
	if _, parenthesized := value.Target.(*syntax.Paren); !parenthesized {
		from = "(" + from + ")"
	}
	c.report(lint.CodeNixManualInheritFrom, a.Span(), fmt.Sprintf("`%s = %s.%s;` is what `inherit %s %s;` says", attr.Name, c.textOf(value.Target.Span()), attr.Name, from, attr.Name),
		c.offer("Use `inherit`", lint.Safe, textedit.Edit{Start: a.Span().Start, End: a.Span().End, NewText: "inherit " + from + " " + attr.Name + ";"}))
}

func (c *checker) legacyLet(l *syntax.LegacyLet) {
	start := l.Span().Start
	keyword := syntax.Span{Start: start, End: start + len(letKeyword)}
	c.report(lint.CodeNixLegacyLet, keyword, "`let { }` is an undocumented form; it means a `rec` set with `.body` selected",
		c.offer("Use `rec { }.body`", lint.Safe,
			textedit.Edit{Start: keyword.Start, End: keyword.End, NewText: recKeyword},
			textedit.Edit{Start: l.Span().End, End: l.Span().End, NewText: "." + scope.LegacyBody}))
}

func (c *checker) collapsibleLet(outer *syntax.Let) {
	inner, nested := outer.Body.(*syntax.Let)
	if !nested || len(outer.Bindings) == 0 || !c.mergeable(outer, inner) {
		return
	}
	start := inner.Span().Start
	var fixes []lint.Fix
	if len(inner.Bindings) > 0 {
		end := outer.Bindings[len(outer.Bindings)-1].Span().End
		first := inner.Bindings[0].Span().Start
		fixes = c.offer("Merge the `let` blocks", lint.Safe, textedit.Edit{Start: end, End: first, NewText: c.leadingBlank(first)})
	}
	c.report(lint.CodeNixCollapsibleLet, syntax.Span{Start: start, End: start + len(letKeyword)}, "this `let` is the whole body of another, so the two can be one", fixes)
}

func (c *checker) leadingBlank(at int) string {
	text := c.tree.Text
	start := at
	for start > 0 && (text[start-1] == ' ' || text[start-1] == '\t') {
		start--
	}
	if start > 0 && text[start-1] == '\n' {
		return "\n" + text[start:at]
	}
	return " "
}

func (c *checker) mergeable(outer, inner *syntax.Let) bool {
	innerScope, known := c.table.ScopeOf(inner)
	if !known {
		return true
	}
	names := make(map[string]bool, len(innerScope.Defs))
	for _, d := range innerScope.Defs {
		if d.Implicit {
			return false
		}
		names[d.Name] = true
	}
	if outerScope, ok := c.table.ScopeOf(outer); ok {
		for _, d := range outerScope.Defs {
			if names[d.Name] {
				return false
			}
		}
	}
	bindings := syntax.Span{Start: outer.Bindings[0].Span().Start, End: outer.Bindings[len(outer.Bindings)-1].Span().End}
	for _, u := range c.usesIn(bindings) {
		if names[u.Name] {
			return false
		}
	}
	return true
}

func (c *checker) usesIn(span syntax.Span) []scope.Use {
	uses := c.table.Uses()
	first := lowerUse(uses, span.Start)
	last := lowerUse(uses, span.End)
	return uses[first:last]
}

func lowerUse(uses []scope.Use, at int) int {
	lo, hi := 0, len(uses)
	for lo < hi {
		mid := (lo + hi) / 2
		if uses[mid].Node.Span().Start < at {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

func (c *checker) etaReduction(l *syntax.Lambda, p *syntax.IdentParam) {
	if !c.optional {
		return
	}
	call, ok := l.Body.(*syntax.Apply)
	if !ok || len(call.Args) != 1 {
		return
	}
	fn, fnIdent := call.Fn.(*syntax.Ident)
	arg, argIdent := call.Args[0].(*syntax.Ident)
	if !fnIdent || !argIdent || arg.Name != p.Name.Name || fn.Name == p.Name.Name || c.bindingOwnValue(fn, l) {
		return
	}
	c.report(lint.CodeNixEtaReduction, l.Span(), fmt.Sprintf("this function only passes its argument to `%s`", fn.Name),
		c.offer("Use `"+fn.Name+"`", lint.Review, textedit.Edit{Start: l.Span().Start, End: l.Span().End, NewText: fn.Name}))
}

func (c *checker) bindingOwnValue(fn *syntax.Ident, l *syntax.Lambda) bool {
	ref, resolved := c.table.Of(fn)
	if !resolved || ref.Def == nil {
		return false
	}
	return slices.ContainsFunc(ref.Def.Bindings, func(b syntax.Binding) bool {
		a, ok := b.(*syntax.Assign)
		return ok && syntax.Unparenthesized(a.Value) == syntax.Expr(l)
	})
}

func (c *checker) emptyPattern(l *syntax.Lambda, p *syntax.PatternParam) {
	if !c.optional || len(p.Formals) != 0 || p.Alias != nil || importsModule(l.Body) {
		return
	}
	c.report(lint.CodeNixEmptyPattern, p.Span(), "this pattern names nothing; `_` says the argument goes unread",
		c.offer("Use `_`", lint.Review, textedit.Edit{Start: p.Span().Start, End: p.Span().End, NewText: "_"}))
}

func importsModule(body syntax.Expr) bool {
	set, ok := body.(*syntax.AttrSet)
	if !ok {
		return false
	}
	return slices.ContainsFunc(set.Bindings, func(b syntax.Binding) bool {
		a, isAssign := b.(*syntax.Assign)
		if !isAssign || len(a.Path) != 1 {
			return false
		}
		attr, isIdent := a.Path[0].(*syntax.IdentAttr)
		return isIdent && attr.Name == importsAttr
	})
}

func (c *checker) redundantPatternBind(p *syntax.PatternParam) {
	if len(p.Formals) != 0 || !p.Ellipsis || p.Alias == nil {
		return
	}
	name := p.Alias.Name
	c.report(lint.CodeNixRedundantPatternBind, p.Span(), fmt.Sprintf("`%s@{ ... }` captures nothing that `%s` alone does not", name, name),
		c.offer("Use `"+name+"`", lint.Review, textedit.Edit{Start: p.Span().Start, End: p.Span().End, NewText: name}))
}

func (c *checker) unquotedURI(u *syntax.URI) {
	span := u.Span()
	c.report(lint.CodeNixUnquotedURI, span, "an unquoted URI is a deprecated form of a string",
		c.offer("Quote the URI", lint.Safe, textedit.Edit{Start: span.Start, End: span.End, NewText: `"` + c.textOf(span) + `"`}))
}

func (c *checker) builtinName(e syntax.Expr) (string, bool) {
	switch e := e.(type) {
	case *syntax.Select:
		id, ok := e.Target.(*syntax.Ident)
		if !ok || id.Name != builtinsName || len(e.Path) != 1 || !c.global(id) {
			return "", false
		}
		attr, ok := e.Path[0].(*syntax.IdentAttr)
		if !ok {
			return "", false
		}
		return attr.Name, true
	case *syntax.Ident:
		if !c.global(e) {
			return "", false
		}
		return strings.TrimPrefix(e.Name, builtinPrefix), true
	}
	return "", false
}

func (c *checker) global(id *syntax.Ident) bool {
	ref, resolved := c.table.Of(id)
	return resolved && ref.Kind == scope.Global
}

func (c *checker) withBuiltins(id *syntax.Ident) bool {
	ref, resolved := c.table.Of(id)
	if !resolved || ref.Kind != scope.Dynamic {
		return false
	}
	for _, w := range c.table.Withs(ref.With) {
		if target, ok := w.Scope.(*syntax.Ident); ok && target.Name == builtinsName && c.global(target) {
			return true
		}
	}
	return false
}

func (c *checker) selection(n *syntax.Select) {
	name, ok := c.builtinName(n)
	if !ok {
		c.pkgsLib(n)
		return
	}
	c.builtinRead(name, n.Span())
}

func (c *checker) identifier(id *syntax.Ident) {
	if !c.withBuiltins(id) {
		if name, ok := c.builtinName(id); ok && strings.HasPrefix(id.Name, builtinPrefix) {
			c.builtinRead(name, id.Span())
		}
		return
	}
	c.builtinRead(id.Name, id.Span())
}

func (c *checker) builtinRead(name string, span syntax.Span) {
	if name == "toPath" {
		c.report(lint.CodeNixDeprecatedToPath, span, "`toPath` is deprecated; add to a path with `/. + s`, or use the string as it is", nil)
	}
	c.impureBuiltin(name, span)
}

func (c *checker) uselessHasAttr(e *syntax.If) {
	cond, ok := syntax.Unparenthesized(e.Cond).(*syntax.HasAttr)
	if !ok {
		return
	}
	then, ok := syntax.Unparenthesized(e.Then).(*syntax.Select)
	if !ok || then.Default != nil || !sameSelection(cond, then) {
		return
	}
	otherwise := c.textOf(e.Else.Span())
	if !atomic(e.Else) {
		otherwise = "(" + otherwise + ")"
	}
	replacement := c.textOf(then.Span()) + " " + orKeyword + " " + otherwise
	c.report(lint.CodeNixUselessHasAttr, e.Span(), "this `if` is what `or` says",
		c.offer("Use `or`", lint.Safe, textedit.Edit{Start: e.Span().Start, End: e.Span().End, NewText: replacement}))
}

func sameSelection(cond *syntax.HasAttr, then *syntax.Select) bool {
	base, path := flattenedSelect(cond.Target, cond.Path)
	thenBase, thenPath := flattenedSelect(then.Target, then.Path)
	return sameExpr(base, thenBase) && samePath(path, thenPath)
}

func flattenedSelect(target syntax.Expr, path syntax.AttrPath) (syntax.Expr, syntax.AttrPath) {
	inner, ok := syntax.Unparenthesized(target).(*syntax.Select)
	if !ok || inner.Default != nil {
		return target, path
	}
	base, prefix := flattenedSelect(inner.Target, inner.Path)
	return base, slices.Concat(prefix, path)
}

func samePath(a, b syntax.AttrPath) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !sameAttr(a[i], b[i]) {
			return false
		}
	}
	return true
}

func sameAttr(a, b syntax.Attr) bool {
	left, leftStatic := syntax.AttrName(a)
	right, rightStatic := syntax.AttrName(b)
	if leftStatic || rightStatic {
		return leftStatic && rightStatic && left == right
	}
	if x, ok := a.(*syntax.StringAttr); ok {
		y, ok := b.(*syntax.StringAttr)
		return ok && sameExpr(x.Value, y.Value)
	}
	x, xDynamic := a.(*syntax.DynamicAttr)
	y, yDynamic := b.(*syntax.DynamicAttr)
	return xDynamic && yDynamic && sameExpr(x.Value, y.Value)
}

func sameExpr(a, b syntax.Expr) bool {
	a, b = syntax.Unparenthesized(a), syntax.Unparenthesized(b)
	switch a := a.(type) {
	case *syntax.Ident:
		other, ok := b.(*syntax.Ident)
		return ok && a.Name == other.Name
	case *syntax.Select:
		other, ok := b.(*syntax.Select)
		return ok && a.Default == nil && other.Default == nil && sameExpr(a.Target, other.Target) && samePath(a.Path, other.Path)
	case *syntax.String:
		other, ok := b.(*syntax.String)
		return ok && a.Kind == other.Kind && sameParts(a.Parts, other.Parts)
	case *syntax.Apply:
		other, ok := b.(*syntax.Apply)
		if !ok || len(a.Args) != len(other.Args) || !sameExpr(a.Fn, other.Fn) {
			return false
		}
		for i := range a.Args {
			if !sameExpr(a.Args[i], other.Args[i]) {
				return false
			}
		}
		return true
	}
	return false
}

func sameParts(a, b []syntax.Part) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		switch x := a[i].(type) {
		case *syntax.Literal:
			y, ok := b[i].(*syntax.Literal)
			if !ok || x.Text != y.Text {
				return false
			}
		case *syntax.Escape:
			y, ok := b[i].(*syntax.Escape)
			if !ok || x.Text != y.Text {
				return false
			}
		case *syntax.Interpolation:
			y, ok := b[i].(*syntax.Interpolation)
			if !ok || !sameExpr(x.Value, y.Value) {
				return false
			}
		}
	}
	return true
}

func (c *checker) repeatedKeys(set *syntax.AttrSet) {
	if !c.optional || set.Rec {
		return
	}
	counts := map[string]int{}
	for _, b := range set.Bindings {
		if a, ok := b.(*syntax.Assign); ok {
			if attr, ok := a.Path[0].(*syntax.IdentAttr); ok {
				counts[attr.Name]++
			}
		}
	}
	for _, b := range set.Bindings {
		a, ok := b.(*syntax.Assign)
		if !ok {
			continue
		}
		attr, ok := a.Path[0].(*syntax.IdentAttr)
		if !ok || counts[attr.Name] < minRepeatedKey {
			continue
		}
		if len(a.Path) > 1 {
			c.report(lint.CodeNixRepeatedKeys, attr.Span(), fmt.Sprintf("`%s` starts %d bindings here, which one nested set could hold", attr.Name, counts[attr.Name]), nil)
		}
		delete(counts, attr.Name)
	}
}

func OptionalLintFindings(text string, enabled lint.CodeSet) []lint.Finding {
	tree := syntax.Parse(text)
	if len(tree.Errors) > 0 {
		return nil
	}
	table := scope.Resolve(tree)
	kept := slices.DeleteFunc(unsuppressed(tree, newChecker(tree, table, nil, true).run()), func(f lint.Finding) bool {
		return !enabled.Has(f.Code)
	})
	lint.SortHostFindings(kept)
	return kept
}

func OptionalLintCodes() []lint.Code {
	return []lint.Code{lint.CodeNixEtaReduction, lint.CodeNixEmptyPattern, lint.CodeNixRepeatedKeys, lint.CodeNixUsedUnderscore, lint.CodeNixTopLevelWith, lint.CodeNixStoreCopy, lint.CodeNixPkgsLib}
}
