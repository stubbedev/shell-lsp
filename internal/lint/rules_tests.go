package lint

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/internal/textline"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var testsFamily = family{
	codes: map[Code]meta{
		2212: {severity: SeverityStyle, summary: "An empty test is always false."},
		2233: {severity: SeverityStyle, summary: "Parentheses around a condition start a needless subshell."},
		2234: {severity: SeverityStyle, summary: "Parentheses around a test start a needless subshell."},
		2235: {severity: SeverityStyle, summary: "Tests are grouped with ( ), which starts a needless subshell."},
		2236: {severity: SeverityStyle, optional: optionalNegatedConditions, summary: "A negated test operator is written as ! op instead of its opposite."},
		2237: {severity: SeverityStyle, optional: optionalNegatedConditions, summary: "A negated [ ] or [[ ]] test is written as ! instead of using the opposite operator."},
		2243: {severity: SeverityStyle, optional: optionalNullaryConditions, summary: "A test of a command substitution relies on the implicit non-empty check."},
		2244: {severity: SeverityStyle, optional: optionalNullaryConditions, summary: "A test of a string relies on the implicit non-empty check instead of -n."},
		2265: {severity: SeverityError, summary: "A single & between tests runs the first in the background instead of combining them."},
		2266: {severity: SeverityWarning, summary: "A single | between tests pipes between them instead of combining them."},
		2268: {severity: SeverityStyle, summary: "The x prefix in comparisons is an obsolete workaround."},
		2292: {severity: SeverityStyle, optional: optionalDoubleBrackets, summary: "[ ] is used for a test where [[ ]] is available."},
		2314: {severity: SeverityError, summary: "Bats ignores the status of a negated command, so the line cannot fail the test."},
		2315: {severity: SeverityError, summary: "Bats ignores the status of a negated test, so the line cannot fail the test."},
		2331: {severity: SeverityStyle, summary: "-a as a file test is a legacy synonym of -e that is easily confused with the -a conjunction."},
		2335: {severity: SeverityStyle, optional: optionalNegatedConditions, summary: "A negated comparison is written as ! a op b instead of using the inverted operator."},
		2338: {severity: SeverityStyle, optional: optionalDoubleEquals, summary: "= is used for string comparison in a bash test where == is preferred."},
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
			p.checkLegacyPrefix(t.style, bin)
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
	walkTest(t.expr, func(e syntax.TestExpr, _ *syntax.UnaryTest) { p.checkTestExpression(t, e) })
}

func (p *pass) checkTestExpression(t testCommand, e syntax.TestExpr) {
	switch e := e.(type) {
	case *syntax.UnaryTest:
		p.checkNegatedOperand(t, e)
		p.checkLegacyExists(e)
	case *syntax.BinaryTest:
		p.checkLegacyPrefix(t.style, e)
		if e.Op == syntax.TsMatchShort && p.dialect == dialect.Bash {
			at := parser.Offset(e.OpPos)
			p.reportNode(2338, e, "In bash tests, prefer == over = for string comparison.", safeFix("Replace = with ==", textedit.Edit{Start: at, End: at + 1, NewText: "=="}))
		}
	case *syntax.Word:
		p.checkImplicitNonEmpty(e)
	}
}

func (p *pass) checkNegatedOperand(t testCommand, e *syntax.UnaryTest) {
	if e.Op != syntax.TsNot {
		return
	}
	at := posSpan(e.OpPos, 1)
	negation := parser.Span{Start: at.Start, End: parser.Offset(e.X.Pos())}
	switch inner := e.X.(type) {
	case *syntax.UnaryTest:
		if opposite, ok := oppositeStringTests[inner.Op]; ok {
			p.report(2236, at, "Use "+opposite.String()+" instead of ! "+inner.Op.String()+".", p.negationFix(t, negation, inner)...)
		}
	case *syntax.BinaryTest:
		if inverted, ok := invertedComparisons[inner.Op]; ok {
			p.report(2335, at, "Use a "+inverted.String()+" b instead of ! a "+inner.Op.String()+" b.", p.negationFix(t, negation, inner)...)
		}
	}
}

func (p *pass) negationFix(t testCommand, negation parser.Span, test syntax.TestExpr) []Fix {
	if t.malformed() {
		return nil
	}
	var at syntax.Pos
	var from, to string
	var operands []syntax.TestExpr
	switch e := test.(type) {
	case *syntax.UnaryTest:
		opposite, ok := oppositeStringTests[e.Op]
		if !ok {
			return nil
		}
		at, from, to, operands = e.OpPos, e.Op.String(), opposite.String(), []syntax.TestExpr{e.X}
	case *syntax.BinaryTest:
		inverted, ok := invertedComparisons[e.Op]
		if !ok {
			return nil
		}
		at, from, to, operands = e.OpPos, e.Op.String(), inverted.String(), []syntax.TestExpr{e.X, e.Y}
	default:
		return nil
	}
	start := parser.Offset(at)
	if !strings.HasPrefix(p.text[start:], from) || (t.style != doubleBracket && slices.ContainsFunc(operands, mayVanish)) {
		return nil
	}
	return []Fix{safeFix("Replace ! "+from+" with "+to,
		textedit.Edit{Start: negation.Start, End: negation.End},
		textedit.Edit{Start: start, End: start + len(from), NewText: to})}
}

func mayVanish(e syntax.TestExpr) bool {
	w, ok := e.(*syntax.Word)
	return ok && !slices.ContainsFunc(w.Parts, func(part syntax.WordPart) bool {
		switch part.(type) {
		case *syntax.ParamExp, *syntax.CmdSubst:
			return false
		}
		return true
	})
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
	p.report(2331, span, "-a as a file test is a legacy synonym that is easily confused with the -a conjunction; use -e.", safeFix("Replace -a with -e", textedit.Edit{Start: start, End: start + 2, NewText: "-e"}))
}

func (p *pass) preferDoubleBrackets(t testCommand) {
	if !p.dialect.Features().DoubleBracket {
		return
	}
	span, ok := p.span(t.node)
	if !ok {
		return
	}
	var fixes []Fix
	if stmt, ok := p.parent().(*syntax.Stmt); ok {
		if edits, ok := DoubleBrackets(p.text, stmt); ok {
			fixes = append(fixes, safeFix("Convert to [[ ]]", edits...))
		}
	}
	p.report(2292, span, "Prefer [[ ]] over [ ] for tests in this shell.", fixes...)
}

func (p *pass) checkImplicitNonEmpty(w *syntax.Word) {
	parts := flattenQuotes(w.Parts)
	insert := safeFix("Add -n", textedit.Edit{Start: parser.Offset(w.Pos()), End: parser.Offset(w.Pos()), NewText: "-n "})
	if len(parts) == 1 {
		if _, ok := parts[0].(*syntax.CmdSubst); ok {
			p.reportNode(2243, w, "Use -n to test that the command printed something, or run the command directly to test its exit status.", insert)
			return
		}
	}
	if slices.ContainsFunc(parts, isConstantPart) {
		return
	}
	p.reportNode(2244, w, "Use -n to make the non-empty string test explicit.", insert)
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

func (p *pass) checkLegacyPrefix(style bracketStyle, e *syntax.BinaryTest) {
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
	p.reportNode(2268, left, "The x prefix in comparisons is an obsolete workaround; compare the values directly.", Fix{Title: "Remove the x prefixes", Safety: xPrefixSafety(style, left, right), Edits: []textedit.Edit{leftFix, rightFix}})
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

func xPrefixSafety(style bracketStyle, words ...*syntax.Word) Safety {
	if style == doubleBracket {
		return Safe
	}
	for _, w := range words {
		lit, ok := w.Parts[0].(*syntax.Lit)
		if ok && len(lit.Value) == 1 && len(w.Parts) > 1 && mayVanish(&syntax.Word{Parts: w.Parts[1:]}) {
			return Review
		}
	}
	return Safe
}

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
	negation := parser.Span{Start: at.Start, End: parser.Offset(stmt.Cmd.Pos())}
	open, closing := "[ ", " ]"
	if t.style == doubleBracket {
		open, closing = "[[ ", " ]]"
	}
	switch e := t.expr.(type) {
	case *syntax.UnaryTest:
		if opposite, ok := oppositeStringTests[e.Op]; ok {
			p.report(2237, at, "Use "+open+opposite.String()+" .."+closing+" instead of ! "+open+e.Op.String()+" .."+closing+".", p.negationFix(t, negation, e)...)
		}
	case *syntax.BinaryTest:
		if inverted, ok := invertedComparisons[e.Op]; ok {
			p.report(2335, at, "Use "+open+"a "+inverted.String()+" b"+closing+" instead of ! "+open+"a "+e.Op.String()+" b"+closing+".", p.negationFix(t, negation, e)...)
		}
	}
}

func checkBackgroundedTest(p *pass, n syntax.Node) {
	stmt, ok := n.(*syntax.Stmt)
	if !ok || !stmt.Background || !stmt.Semicolon.IsValid() || !isTestCommand(rightmost(stmt)) {
		return
	}
	at := parser.Offset(stmt.Semicolon)
	var fixes []Fix
	if rest := strings.TrimLeft(p.text[at+1:textline.End(p.text, at)], " \t"); rest != "" && !strings.HasPrefix(rest, "#") {
		fixes = append(fixes, reviewFix("Replace & with &&", textedit.Edit{Start: at + 1, End: at + 1, NewText: "&"}))
	}
	p.report(2265, posSpan(stmt.Semicolon, 1), "A single & runs the test in the background instead of combining it; use && for a logical AND.", fixes...)
}

func checkPipedTest(p *pass, n syntax.Node) {
	bin, ok := n.(*syntax.BinaryCmd)
	if !ok || bin.Op != syntax.Pipe || (!isTestCommand(rightmost(bin.X)) && !isTestCommand(leftmost(bin.Y))) {
		return
	}
	at := parser.Offset(bin.OpPos)
	p.report(2266, posSpan(bin.OpPos, 1), "A single | pipes between commands instead of combining tests; use || for a logical OR.",
		reviewFix("Replace | with ||", textedit.Edit{Start: at + 1, End: at + 1, NewText: "|"}))
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
			reviewFix("Run the negation with run !", textedit.Edit{Start: start, End: start, NewText: "run "}))
	}
}

func batsNegationMessage(last bool, advice string) string {
	if last {
		return "As the last command, this negation only decides the test result by accident of position; " + advice + "."
	}
	return "Bats ignores the status of a negated command, so this line can never fail the test; " + advice + "."
}
