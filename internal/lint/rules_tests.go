package lint

import (
	"slices"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

const (
	optionalNegatedConditions = "avoid-negated-conditions"
	optionalNullaryConditions = "avoid-nullary-conditions"
)

var testsFamily = family{
	codes: map[Code]meta{
		2212: {severity: SeverityStyle},
		2233: {severity: SeverityStyle},
		2234: {severity: SeverityStyle},
		2235: {severity: SeverityStyle},
		2236: {severity: SeverityStyle, optional: optionalNegatedConditions},
		2237: {severity: SeverityStyle, optional: optionalNegatedConditions},
		2243: {severity: SeverityStyle, optional: optionalNullaryConditions},
		2244: {severity: SeverityStyle, optional: optionalNullaryConditions},
		2265: {severity: SeverityError},
		2266: {severity: SeverityWarning},
		2268: {severity: SeverityStyle},
		2292: {severity: SeverityStyle, optional: "require-double-brackets"},
		2314: {severity: SeverityError},
		2315: {severity: SeverityError},
		2331: {severity: SeverityStyle},
		2335: {severity: SeverityStyle, optional: optionalNegatedConditions},
		2338: {severity: SeverityStyle, optional: "require-double-equals"},
	},
	rules: []rule{
		{on: nodes(&syntax.CallExpr{}, &syntax.TestClause{}), node: checkTestExpressions},
		{on: nodes(&syntax.Stmt{}), node: checkNegatedTestCommand},
		{on: nodes(&syntax.Stmt{}), node: checkBackgroundedTest},
		{on: nodes(&syntax.BinaryCmd{}), node: checkPipedTest},
		{on: nodes(&syntax.Subshell{}), node: checkSubshellTest},
		{on: nodes(&syntax.TestDecl{}), node: checkBatsNegation},
	},
}

var invertedComparisons = map[syntax.BinTestOperator]syntax.BinTestOperator{
	syntax.TsMatchShort: syntax.TsNoMatch,
	syntax.TsMatch:      syntax.TsNoMatch,
	syntax.TsNoMatch:    syntax.TsMatchShort,
	syntax.TsEql:        syntax.TsNeq,
	syntax.TsNeq:        syntax.TsEql,
	syntax.TsLeq:        syntax.TsGtr,
	syntax.TsGtr:        syntax.TsLeq,
	syntax.TsGeq:        syntax.TsLss,
	syntax.TsLss:        syntax.TsGeq,
}

var oppositeStringTests = map[syntax.UnTestOperator]syntax.UnTestOperator{
	syntax.TsNempStr: syntax.TsEmpStr,
	syntax.TsEmpStr:  syntax.TsNempStr,
}

func checkTestExpressions(p *pass, n syntax.Node) {
	cmd, ok := n.(syntax.Command)
	if !ok {
		return
	}
	t, ok := testCommandOf(cmd)
	if !ok {
		return
	}
	if !t.bracketed() {
		if bin, ok := t.expr.(*syntax.BinaryTest); ok && len(t.args) == 3 && t.name == "test" && bin.Op != syntax.TsMatch {
			p.checkLegacyPrefix(bin)
		}
		return
	}
	if t.style == singleBracket {
		p.preferDoubleBrackets(t)
	}
	if t.expr == nil && !t.unterminated {
		p.reportNode(2212, t.node, "This test is empty and always false; write false if that is intended.")
		return
	}
	walkTest(t.expr, func(e syntax.TestExpr, _ *syntax.UnaryTest) { p.checkTestExpression(e) })
}

func (p *pass) checkTestExpression(e syntax.TestExpr) {
	switch e := e.(type) {
	case *syntax.UnaryTest:
		p.checkNegatedOperand(e)
		p.checkLegacyExists(e)
	case *syntax.BinaryTest:
		p.checkLegacyPrefix(e)
		if e.Op == syntax.TsMatchShort && p.dialect == dialect.Bash {
			p.reportNode(2338, e, "In bash tests, prefer == over = for string comparison.")
		}
	case *syntax.Word:
		p.checkImplicitNonEmpty(e)
	}
}

func (p *pass) checkNegatedOperand(e *syntax.UnaryTest) {
	if e.Op != syntax.TsNot {
		return
	}
	at := posSpan(e.OpPos, 1)
	switch inner := e.X.(type) {
	case *syntax.UnaryTest:
		if opposite, ok := oppositeStringTests[inner.Op]; ok {
			p.report(2236, at, "Use "+opposite.String()+" instead of ! "+inner.Op.String()+".")
		}
	case *syntax.BinaryTest:
		if inverted, ok := invertedComparisons[inner.Op]; ok {
			p.report(2335, at, "Use a "+inverted.String()+" b instead of ! a "+inner.Op.String()+" b.")
		}
	}
}

func (p *pass) checkLegacyExists(e *syntax.UnaryTest) {
	start := parser.Offset(e.OpPos)
	if e.Op != syntax.TsExists || start+2 > len(p.text) || p.text[start:start+2] != "-a" {
		return
	}
	span, ok := p.span(e)
	if !ok {
		return
	}
	p.report(2331, span, "-a as a file test is a legacy synonym that is easily confused with the -a conjunction; use -e.", textedit.Edit{Start: start, End: start + 2, NewText: "-e"})
}

func (p *pass) preferDoubleBrackets(t testCommand) {
	if !p.dialect.Features().DoubleBracket {
		return
	}
	span, ok := p.span(t.node)
	if !ok {
		return
	}
	var fix []textedit.Edit
	if stmt, ok := p.parent().(*syntax.Stmt); ok {
		fix, _ = DoubleBrackets(p.text, stmt)
	}
	p.report(2292, span, "Prefer [[ ]] over [ ] for tests in this shell.", fix...)
}

func (p *pass) checkImplicitNonEmpty(w *syntax.Word) {
	parts := flattenQuotes(w.Parts)
	insert := []textedit.Edit{{Start: parser.Offset(w.Pos()), End: parser.Offset(w.Pos()), NewText: "-n "}}
	if len(parts) == 1 {
		if _, ok := parts[0].(*syntax.CmdSubst); ok {
			p.reportNode(2243, w, "Use -n to test that the command printed something, or run the command directly to test its exit status.", insert...)
			return
		}
	}
	if slices.ContainsFunc(parts, isConstantPart) {
		return
	}
	p.reportNode(2244, w, "Use -n to make the non-empty string test explicit.", insert...)
}

func flattenQuotes(parts []syntax.WordPart) []syntax.WordPart {
	if !slices.ContainsFunc(parts, func(part syntax.WordPart) bool { _, ok := part.(*syntax.DblQuoted); return ok }) {
		return parts
	}
	var out []syntax.WordPart
	for _, part := range parts {
		if dq, ok := part.(*syntax.DblQuoted); ok {
			out = append(out, dq.Parts...)
			continue
		}
		out = append(out, part)
	}
	return out
}

func isConstantPart(part syntax.WordPart) bool {
	switch part.(type) {
	case *syntax.Lit, *syntax.SglQuoted:
		return true
	}
	return false
}

func (p *pass) checkLegacyPrefix(e *syntax.BinaryTest) {
	if e.Op != syntax.TsMatchShort && e.Op != syntax.TsMatch && e.Op != syntax.TsNoMatch {
		return
	}
	left, ok := e.X.(*syntax.Word)
	if !ok {
		return
	}
	right, ok := e.Y.(*syntax.Word)
	if !ok {
		return
	}
	leftFix, ok := xPrefixRemoval(left)
	if !ok {
		return
	}
	rightFix, ok := xPrefixRemoval(right)
	if !ok {
		return
	}
	p.reportNode(2268, left, "The x prefix in comparisons is an obsolete workaround; compare the values directly.", leftFix, rightFix)
}

func xPrefixRemoval(w *syntax.Word) (textedit.Edit, bool) {
	start := parser.Offset(w.Pos())
	switch first := w.Parts[0].(type) {
	case *syntax.Lit:
		if !startsWithX(first.Value) {
			return textedit.Edit{}, false
		}
		if len(w.Parts) == 1 && len(first.Value) == 1 {
			return textedit.Edit{Start: start, End: start + 1, NewText: `""`}, true
		}
		return textedit.Edit{Start: start, End: start + 1}, true
	case *syntax.SglQuoted:
		if first.Dollar || !startsWithX(first.Value) {
			return textedit.Edit{}, false
		}
		return textedit.Edit{Start: start + 1, End: start + 2}, true
	case *syntax.DblQuoted:
		if first.Dollar || len(first.Parts) == 0 {
			return textedit.Edit{}, false
		}
		lit, ok := first.Parts[0].(*syntax.Lit)
		if !ok || !startsWithX(lit.Value) {
			return textedit.Edit{}, false
		}
		return textedit.Edit{Start: start + 1, End: start + 2}, true
	}
	return textedit.Edit{}, false
}

func startsWithX(s string) bool { return s != "" && (s[0] == 'x' || s[0] == 'X') }

func checkNegatedTestCommand(p *pass, n syntax.Node) {
	stmt, ok := n.(*syntax.Stmt)
	if !ok || !stmt.Negated {
		return
	}
	t, ok := testCommandOf(stmt.Cmd)
	if !ok || !t.bracketed() {
		return
	}
	at := posSpan(stmt.Pos(), 1)
	open, closing := "[ ", " ]"
	if t.style == doubleBracket {
		open, closing = "[[ ", " ]]"
	}
	switch e := t.expr.(type) {
	case *syntax.UnaryTest:
		if opposite, ok := oppositeStringTests[e.Op]; ok {
			p.report(2237, at, "Use "+open+opposite.String()+" .."+closing+" instead of ! "+open+e.Op.String()+" .."+closing+".")
		}
	case *syntax.BinaryTest:
		if inverted, ok := invertedComparisons[e.Op]; ok {
			p.report(2335, at, "Use "+open+"a "+inverted.String()+" b"+closing+" instead of ! "+open+"a "+e.Op.String()+" b"+closing+".")
		}
	}
}

func checkBackgroundedTest(p *pass, n syntax.Node) {
	stmt, ok := n.(*syntax.Stmt)
	if !ok || !stmt.Background || !stmt.Semicolon.IsValid() || !isTestCommand(rightmost(stmt)) {
		return
	}
	at := parser.Offset(stmt.Semicolon)
	p.report(2265, posSpan(stmt.Semicolon, 1), "A single & runs the test in the background instead of combining it; use && for a logical AND.",
		textedit.Edit{Start: at + 1, End: at + 1, NewText: "&"})
}

func checkPipedTest(p *pass, n syntax.Node) {
	bin, ok := n.(*syntax.BinaryCmd)
	if !ok || bin.Op != syntax.Pipe || (!isTestCommand(rightmost(bin.X)) && !isTestCommand(leftmost(bin.Y))) {
		return
	}
	at := parser.Offset(bin.OpPos)
	p.report(2266, posSpan(bin.OpPos, 1), "A single | pipes between commands instead of combining tests; use || for a logical OR.",
		textedit.Edit{Start: at + 1, End: at + 1, NewText: "|"})
}

func checkSubshellTest(p *pass, n syntax.Node) {
	sub, ok := n.(*syntax.Subshell)
	if !ok || !onlyTests(sub.Stmts) || hasSideEffects(sub) {
		return
	}
	stmt, _ := p.parent().(*syntax.Stmt)
	switch {
	case stmt != nil && len(stmt.Redirs) == 0 && !stmt.Negated && !stmt.Background && isConditionalOwner(p.ancestor(1)):
		p.reportNode(2233, sub, "The parentheses around this condition start a needless subshell; remove them.")
	case len(sub.Stmts) == 1 && !sub.Stmts[0].Negated && isTestCommand(sub.Stmts[0]) && !isFunction(p.ancestor(1)):
		p.reportNode(2234, sub, "The parentheses around this test start a needless subshell; remove them.")
	default:
		p.reportNode(2235, sub, "Group these tests with { ..; } instead of ( .. ) to avoid starting a subshell.")
	}
}

func isConditionalOwner(n syntax.Node) bool {
	switch n.(type) {
	case *syntax.IfClause, *syntax.WhileClause:
		return true
	}
	return false
}

func isFunction(n syntax.Node) bool {
	_, ok := n.(*syntax.FuncDecl)
	return ok
}

func onlyTests(stmts []*syntax.Stmt) bool {
	return len(stmts) > 0 && !slices.ContainsFunc(stmts, func(s *syntax.Stmt) bool { return !isTestStructure(s) })
}

func isTestStructure(stmt *syntax.Stmt) bool {
	if stmt.Background || stmt.Coprocess {
		return false
	}
	switch cmd := stmt.Cmd.(type) {
	case *syntax.BinaryCmd:
		return (cmd.Op == syntax.AndStmt || cmd.Op == syntax.OrStmt) && isTestStructure(cmd.X) && isTestStructure(cmd.Y)
	case *syntax.Block:
		return onlyTests(cmd.Stmts)
	case *syntax.Subshell:
		return onlyTests(cmd.Stmts)
	}
	return isTestCommand(stmt)
}

func hasSideEffects(root syntax.Node) bool {
	found := false
	parser.Walk(root, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.ParamExp:
			found = found || assignsParameter(n)
		case *syntax.UnaryArithm:
			found = found || n.Op == syntax.Inc || n.Op == syntax.Dec
		case *syntax.BinaryArithm:
			found = found || parser.IsArithmeticAssignment(n.Op)
		case *syntax.CmdSubst:
			found = found || n.TempFile || n.ReplyVar
		}
		return !found
	})
	return found
}

func checkBatsNegation(p *pass, n syntax.Node) {
	decl, ok := n.(*syntax.TestDecl)
	if !ok || decl.Body == nil {
		return
	}
	block, ok := decl.Body.Cmd.(*syntax.Block)
	if !ok {
		return
	}
	for i, stmt := range block.Stmts {
		if !stmt.Negated {
			continue
		}
		last := i == len(block.Stmts)-1
		severity := SeverityError
		if last {
			severity = SeverityStyle
		}
		start := parser.Offset(stmt.Pos())
		at := posSpan(stmt.Pos(), 1)
		if t, ok := testCommandOf(stmt.Cmd); ok && t.bracketed() {
			p.reportWithSeverity(2315, severity, at, batsNegationMessage(last, "fold the ! into the test, e.g. [ ! -e file ] or [[ ! -e file ]]"))
			continue
		}
		p.reportWithSeverity(2314, severity, at, batsNegationMessage(last, "use run ! command so bats checks the status"),
			textedit.Edit{Start: start, End: start, NewText: "run "})
	}
}

func batsNegationMessage(last bool, advice string) string {
	if last {
		return "As the last command, this negation only decides the test result by accident of position; " + advice + "."
	}
	return "Bats ignores the status of a negated command, so this line can never fail the test; " + advice + "."
}
