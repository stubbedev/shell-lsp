package nix

import (
	"fmt"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

const (
	orKeyword    = "or"
	flakeOutput  = "outputs"
	unusedPrefix = "_"
	trueLiteral  = "true"
	falseLiteral = "false"
	recKeyword   = "rec"
	letKeyword   = "let"
)

type parenRole uint8

const (
	parenAnywhere parenRole = iota
	parenAtExpression
	parenRequired
)

type checker struct {
	tree     *syntax.File
	table    *scope.Table
	parens   map[*syntax.Paren]parenRole
	names    map[string]bool
	outputs  *syntax.Lambda
	findings []lint.Finding
}

func check(tree *syntax.File, table *scope.Table, extra []lint.Finding) []lint.Finding {
	if len(tree.Errors) > 0 {
		return nil
	}
	c := &checker{tree: tree, table: table, parens: map[*syntax.Paren]parenRole{}, names: map[string]bool{}, outputs: flakeOutputs(tree.Root), findings: extra}
	c.uses()
	c.duplicates()
	c.unusedLets()
	c.atExpression(tree.Root)
	syntax.Inspect(tree.Root, c.visit)
	kept := unsuppressed(tree, c.findings)
	lint.SortHostFindings(kept)
	return kept
}

func flakeOutputs(root syntax.Expr) *syntax.Lambda {
	set, ok := root.(*syntax.AttrSet)
	if !ok {
		return nil
	}
	for _, b := range set.Bindings {
		a, ok := b.(*syntax.Assign)
		if !ok || len(a.Path) != 1 {
			continue
		}
		if name, static := syntax.AttrName(a.Path[0]); static && name == flakeOutput {
			lambda, _ := a.Value.(*syntax.Lambda)
			return lambda
		}
	}
	return nil
}

func (c *checker) report(code lint.Code, span syntax.Span, message string, fixes []lint.Fix) {
	f := lint.HostFinding(code, span, message)
	f.Fixes = fixes
	c.findings = append(c.findings, f)
}

func (c *checker) uses() {
	for _, u := range c.table.Uses() {
		c.names[u.Name] = true
		span := u.Node.Span()
		if u.Ref.Kind == scope.Undefined {
			c.report(lint.CodeNixUndefinedVariable, span, fmt.Sprintf("`%s` is not defined here, so Nix rejects the file", u.Name), nil)
		}
		if u.Name == orKeyword && !u.Inherit {
			c.report(lint.CodeNixOrIdentifier, span, "`or` is read as a variable here, which only this one position still allows and Nix warns will change", nil)
		}
	}
}

func (c *checker) duplicates() {
	for _, d := range c.table.Duplicates() {
		c.report(lint.CodeNixDuplicateAttribute, d.Attr.Span(), fmt.Sprintf("attribute `%s` is defined more than once, so Nix rejects the file", d.Path), nil)
	}
}

func (c *checker) unusedLets() {
	for _, d := range c.table.Defs() {
		if d.Scope.Kind != scope.LetScope || d.Implicit || strings.HasPrefix(d.Name, unusedPrefix) || usedOutsideItself(d) {
			continue
		}
		c.report(lint.CodeNixUnusedLet, d.Binders[0].Span(), fmt.Sprintf("`%s` is bound but nothing reads it", d.Name), c.removal(d))
	}
}

func usedOutsideItself(d *scope.Def) bool {
	var own []syntax.Span
	for _, b := range d.Bindings {
		if _, ok := b.(*syntax.Assign); ok {
			own = append(own, b.Span())
		}
	}
	i := 0
	for _, u := range d.Uses() {
		at := u.Node.Span().Start
		for i < len(own) && own[i].End <= at {
			i++
		}
		if i == len(own) || at < own[i].Start {
			return true
		}
	}
	return false
}

func (c *checker) atExpression(exprs ...syntax.Expr) {
	for _, e := range exprs {
		if p, ok := e.(*syntax.Paren); ok {
			c.parens[p] = parenAtExpression
		}
	}
}

func (c *checker) visit(n syntax.Node) bool {
	switch n := n.(type) {
	case *syntax.Paren:
		c.atExpression(n.Inner)
		c.paren(n)
	case *syntax.If:
		c.atExpression(n.Cond, n.Then, n.Else)
		c.redundantIf(n)
	case *syntax.AttrSet:
		c.redundantRec(n)
	case *syntax.Assign:
		c.atExpression(n.Value)
		c.spelledInherit(n)
	case *syntax.Let:
		c.atExpression(n.Body)
		c.emptyLet(n)
	case *syntax.Inherit:
		if n.From != nil {
			c.parens[n.From] = parenRequired
		}
		c.emptyInherit(n)
	case *syntax.Lambda:
		c.atExpression(n.Body)
		c.lambda(n)
	case *syntax.With:
		c.atExpression(n.Scope, n.Body)
	case *syntax.Assert:
		c.atExpression(n.Cond, n.Body)
	case *syntax.Interpolation:
		c.atExpression(n.Value)
	case *syntax.DynamicAttr:
		c.atExpression(n.Value)
	case *syntax.Formal:
		if n.Default != nil {
			c.atExpression(n.Default)
		}
	case *syntax.Select:
		if p, ok := n.Target.(*syntax.Paren); ok {
			if _, selected := p.Inner.(*syntax.Select); selected {
				c.parens[p] = parenRequired
			}
		}
	}
	return true
}

func atomic(e syntax.Expr) bool {
	switch e := e.(type) {
	case *syntax.Ident:
		return e.Name != orKeyword
	case *syntax.Select:
		return e.Default == nil
	case *syntax.Int, *syntax.Float, *syntax.URI, *syntax.String, *syntax.Path, *syntax.List, *syntax.AttrSet, *syntax.Paren:
		return true
	}
	return false
}

func (c *checker) paren(p *syntax.Paren) {
	role := c.parens[p]
	if role == parenRequired || role == parenAnywhere && !atomic(p.Inner) || !c.separated(p.Span()) {
		return
	}
	c.report(lint.CodeNixUselessParens, p.Span(), "these parentheses change nothing", c.offer("Remove the parentheses", lint.Safe, c.withoutParens(p)...))
}

func (c *checker) redundantIf(e *syntax.If) {
	then, thenLiteral := c.booleanLiteral(e.Then)
	otherwise, elseLiteral := c.booleanLiteral(e.Else)
	if !thenLiteral || !elseLiteral || then == otherwise {
		return
	}
	replacement := textedit.Edit{Start: e.Span().Start, End: e.Span().End}
	title := "Use the condition itself"
	if then {
		cond := e.Cond
		if p, ok := cond.(*syntax.Paren); ok {
			cond = p.Inner
		}
		replacement.NewText = c.textOf(cond.Span())
	} else {
		title = "Negate the condition"
		replacement.NewText = "!" + c.negatable(e.Cond)
	}
	c.report(lint.CodeNixRedundantIf, e.Span(), "this `if` yields its own condition as a Boolean", c.offer(title, lint.Safe, replacement))
}

func (c *checker) booleanLiteral(e syntax.Expr) (bool, bool) {
	id, ok := e.(*syntax.Ident)
	if !ok {
		return false, false
	}
	ref, resolved := c.table.Of(id)
	if !resolved || ref.Kind != scope.Global {
		return false, false
	}
	switch id.Name {
	case trueLiteral:
		return true, true
	case falseLiteral:
		return false, true
	}
	return false, false
}

func (c *checker) negatable(e syntax.Expr) string {
	switch e.(type) {
	case *syntax.Ident, *syntax.Select, *syntax.Apply, *syntax.HasAttr, *syntax.Paren:
		return c.textOf(e.Span())
	}
	return "(" + c.textOf(e.Span()) + ")"
}

func (c *checker) redundantRec(set *syntax.AttrSet) {
	if !set.Rec {
		return
	}
	if s, effective := c.table.ScopeOf(set); effective {
		for _, d := range s.Defs {
			if d.Used() {
				return
			}
		}
	}
	start := set.Span().Start
	keyword := syntax.Span{Start: start, End: start + len(recKeyword)}
	c.report(lint.CodeNixRedundantRec, keyword, "nothing in this set reads one of its own attributes, so `rec` does nothing",
		c.offer("Remove `rec`", lint.Safe, textedit.Edit{Start: start, End: c.skipBlank(keyword.End)}))
}

func (c *checker) spelledInherit(a *syntax.Assign) {
	if len(a.Path) != 1 {
		return
	}
	attr, isIdent := a.Path[0].(*syntax.IdentAttr)
	value, isVariable := a.Value.(*syntax.Ident)
	if !isIdent || !isVariable || attr.Name != value.Name || attr.Name == orKeyword || attr.Name == scope.CurPos {
		return
	}
	ref, resolved := c.table.Of(value)
	if !resolved || ref.Kind == scope.Undefined {
		return
	}
	if own, bound := c.table.Binder(attr); bound && ref.Def == own {
		return
	}
	c.report(lint.CodeNixSpelledInherit, a.Span(), fmt.Sprintf("`%s = %s;` is what `inherit %s;` says", attr.Name, attr.Name, attr.Name),
		c.offer("Use `inherit "+attr.Name+";`", lint.Safe, textedit.Edit{Start: a.Span().Start, End: a.Span().End, NewText: "inherit " + attr.Name + ";"}))
}

func (c *checker) emptyLet(l *syntax.Let) {
	if len(l.Bindings) > 0 {
		return
	}
	start := l.Span().Start
	c.report(lint.CodeNixEmptyBinding, syntax.Span{Start: start, End: start + len(letKeyword)}, "this `let` binds nothing",
		c.offer("Remove the empty `let`", lint.Safe, textedit.Edit{Start: start, End: l.Body.Span().Start}))
}

func (c *checker) emptyInherit(i *syntax.Inherit) {
	if len(i.Attrs) > 0 {
		return
	}
	c.report(lint.CodeNixEmptyBinding, i.Span(), "this `inherit` binds nothing", c.offer("Remove the empty `inherit`", lint.Safe, c.statementRemoval(i.Span())))
}

func (c *checker) lambda(l *syntax.Lambda) {
	switch p := l.Param.(type) {
	case *syntax.IdentParam:
		c.unusedArgument(p)
	case *syntax.PatternParam:
		c.pattern(l, p)
	}
}

func (c *checker) unusedArgument(p *syntax.IdentParam) {
	d, bound := c.table.Binder(p.Name)
	if !bound || d.Used() || strings.HasPrefix(d.Name, unusedPrefix) {
		return
	}
	var fixes []lint.Fix
	if renamed := unusedPrefix + d.Name; !c.names[renamed] {
		fixes = c.offer("Rename to `"+renamed+"`", lint.Review, textedit.Edit{Start: p.Name.Span().Start, End: p.Name.Span().End, NewText: renamed})
	}
	c.report(lint.CodeNixUnusedArgument, p.Name.Span(), fmt.Sprintf("the function never reads its argument `%s`", d.Name), fixes)
}

func (c *checker) pattern(l *syntax.Lambda, p *syntax.PatternParam) {
	c.duplicateFormals(p)
	aliasRead := false
	if p.Alias != nil {
		aliasRead = c.alias(p)
	}
	if aliasRead || l == c.outputs && !p.Ellipsis {
		return
	}
	safety := lint.Review
	if p.Ellipsis {
		safety = lint.Safe
	}
	for i, f := range p.Formals {
		d, bound := c.table.Binder(f.Name)
		if !bound || d.Binders[0] != syntax.Node(f.Name) || d.Used() || strings.HasPrefix(d.Name, unusedPrefix) {
			continue
		}
		c.report(lint.CodeNixUnusedFormal, f.Name.Span(), fmt.Sprintf("the function never reads its argument `%s`", d.Name),
			c.offer("Remove `"+d.Name+"` from the pattern", safety, c.formalRemoval(p, i)))
	}
}

func (c *checker) alias(p *syntax.PatternParam) bool {
	d, bound := c.table.Binder(p.Alias)
	if !bound {
		return false
	}
	if d.Used() {
		return true
	}
	if d.Binders[0] == syntax.Node(p.Alias) && !strings.HasPrefix(d.Name, unusedPrefix) {
		c.report(lint.CodeNixUnusedArgument, p.Alias.Span(), fmt.Sprintf("the function never reads its `@` name `%s`", d.Name),
			c.offer("Remove `"+d.Name+"@`", lint.Safe, c.aliasRemoval(p)))
	}
	return false
}

func (c *checker) duplicateFormals(p *syntax.PatternParam) {
	for i, f := range p.Formals {
		d, bound := c.table.Binder(f.Name)
		if !bound || d.Binders[0] == syntax.Node(f.Name) {
			continue
		}
		var fixes []lint.Fix
		if d.Kind == scope.FormalDef {
			safety := lint.Safe
			if f.Default != nil {
				safety = lint.Review
			}
			fixes = c.offer("Remove this `"+d.Name+"`", safety, c.formalRemoval(p, i))
		}
		c.report(lint.CodeNixDuplicateFormal, f.Name.Span(), fmt.Sprintf("the pattern already names `%s`, so Nix rejects the file", d.Name), fixes)
	}
	if p.Alias == nil || p.AliasFirst {
		return
	}
	if d, bound := c.table.Binder(p.Alias); bound && d.Binders[0] != syntax.Node(p.Alias) {
		c.report(lint.CodeNixDuplicateFormal, p.Alias.Span(), fmt.Sprintf("the pattern already names `%s`, so Nix rejects the file", d.Name), nil)
	}
}
