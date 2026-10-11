package lint

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var testLogicFamily = family{
	codes: map[Code]meta{
		2053: {severity: SeverityWarning, summary: "An unquoted right-hand side of == or != in [[ ]] is matched as a glob pattern."},
		2055: {severity: SeverityWarning, summary: "A value always differs from at least one of two others, so a != ... -o != ... test is always true."},
		2056: {severity: SeverityWarning, summary: "A value always differs from at least one of two others, so a != ... || != ... list is always true."},
		2057: {severity: SeverityWarning, summary: "A comparison uses an operator that test does not understand."},
		2058: {severity: SeverityWarning, summary: "A test uses an operator that does not exist."},
		2065: {severity: SeverityWarning, summary: "> or < inside test is a file redirection, not a comparison."},
		2081: {severity: SeverityError, summary: "[ ] compares a glob pattern as literal text and never matches it."},
		2252: {severity: SeverityWarning, summary: "A value always differs from at least one of several constants, so a || chain of != tests is always true."},
		2333: {severity: SeverityWarning, summary: "A value cannot equal two different constants at once, so the condition is always false."},
		2334: {severity: SeverityWarning, summary: "A value cannot equal two different numbers at once, so the && is always false."},
	},
	commands: []commandRule{
		{match: matchBase, names: []string{"test"}, check: checkTestRedirect},
	},
	rules: []rule{
		{on: nodes(&syntax.BinaryCmd{}, &syntax.CallExpr{}, &syntax.TestClause{}), node: checkTestLogic},
		{on: nodes(&syntax.BinaryCmd{}), node: checkChainedTests},
		{on: nodes(&syntax.BinaryArithm{}), node: checkArithmeticContradiction},
		{finish: checkRejectedUnknownOperators},
	},
}

type comparison struct {
	op          string
	left, right *syntax.Word
	at          parser.Span
}

func comparisonOf(expr syntax.TestExpr) (comparison, bool) {
	bin, ok := expr.(*syntax.BinaryTest)
	if !ok || bin.Op == syntax.AndTest || bin.Op == syntax.OrTest {
		return comparison{}, false
	}
	left, ok := bin.X.(*syntax.Word)
	if !ok {
		return comparison{}, false
	}
	right, ok := bin.Y.(*syntax.Word)
	if !ok {
		return comparison{}, false
	}
	op := bin.Op.String()
	return comparison{op: op, left: left, right: right, at: posSpan(bin.OpPos, len(op))}, true
}

func checkTestLogic(p *pass, n syntax.Node) {
	switch n := n.(type) {
	case *syntax.BinaryCmd:
		if t, ok := splitBracketTest(n); ok {
			p.checkTestCommand(t)
		}
	case syntax.Command:
		if t, ok := testCommandOf(n); ok && t.bracketed() {
			p.checkTestCommand(t)
		}
	}
}

func splitBracketTest(cmd *syntax.BinaryCmd) (testCommand, bool) {
	if cmd.Op != syntax.AndStmt && cmd.Op != syntax.OrStmt {
		return testCommand{}, false
	}
	head, ok := testCommandOf(cmd.X.Cmd)
	if !ok || head.style != singleBracket || !head.unterminated || head.expr == nil || !head.complete {
		return testCommand{}, false
	}
	words, ok := bracketTailWords(cmd.Y.Cmd)
	if !ok || len(words) == 0 {
		return testCommand{}, false
	}
	tail := testOf(singleBracket, words)
	if !tail.complete {
		return testCommand{}, false
	}
	op := syntax.OrTest
	if cmd.Op == syntax.AndStmt {
		op = syntax.AndTest
	}
	expr := &syntax.BinaryTest{OpPos: cmd.OpPos, Op: op, X: head.expr, Y: tail.expr}
	return testCommand{style: singleBracket, expr: expr, opaque: append(head.opaque, tail.opaque...), complete: true}, true
}

func (p *pass) checkTestCommand(t testCommand) {
	if !t.complete {
		return
	}
	if t.style == singleBracket {
		for _, op := range t.opaque {
			p.reportUnknownTestOperator(op)
		}
	}
	walkTest(t.expr, func(e syntax.TestExpr, _ *syntax.UnaryTest) {
		bin, ok := e.(*syntax.BinaryTest)
		if !ok {
			return
		}
		if c, ok := comparisonOf(bin); ok {
			p.checkPatternOperand(t.style, c)
			return
		}
		left, okLeft := comparisonOf(bin.X)
		right, okRight := comparisonOf(bin.Y)
		if okLeft && okRight {
			p.checkContradiction(bin.Op == syntax.AndTest, t.style == doubleBracket, posSpan(bin.OpPos, 2), left, right)
		}
	})
}

func (p *pass) reportUnknownTestOperator(op opaqueTest) {
	if !isOperatorShaped(op.name) {
		return
	}
	span, ok := p.span(op.op)
	if !ok {
		return
	}
	if op.unary() {
		p.report(2058, span, op.name+" is not a test operator, so [ ] cannot evaluate this condition; check the spelling (see help test).")
		return
	}
	p.report(2057, span, op.name+" is not a comparison operator that test understands; check the spelling (see help test).")
}

func (p *pass) checkPatternOperand(style bracketStyle, c comparison) {
	if testOperators[c.op].kind != equalityTest {
		return
	}
	span, ok := p.span(c.right)
	if !ok {
		return
	}
	text := p.text[span.Start:span.End]
	switch style {
	case doubleBracket:
		if pe, ok := solePartUnquoted(c.right).(*syntax.ParamExp); ok && p.implicitlyGlobbed(pe) {
			p.report(2053, span, "The unquoted "+text+" on the right of "+c.op+" is matched as a glob pattern; quote it to compare literally, or keep it unquoted only when it deliberately holds a pattern.")
		}
	case singleBracket:
		if !isGlob(c.right) {
			return
		}
		advice := "use a case statement to match patterns"
		switch p.dialect {
		case dialect.Bash, dialect.Ksh, dialect.Zsh:
			advice = "use [[ ]] or a case statement to match patterns"
		}
		p.report(2081, span, "[ ] compares "+text+" as literal text, it never matches it as a glob; "+advice+".")
	}
}

func solePartUnquoted(w *syntax.Word) syntax.WordPart {
	if len(w.Parts) != 1 {
		return nil
	}
	return w.Parts[0]
}

func checkChainedTests(p *pass, n syntax.Node) {
	cmd, ok := n.(*syntax.BinaryCmd)
	if !ok || (cmd.Op != syntax.AndStmt && cmd.Op != syntax.OrStmt) {
		return
	}
	first := cmd.X
	for {
		inner, ok := first.Cmd.(*syntax.BinaryCmd)
		if !ok || inner.Op != cmd.Op {
			break
		}
		first = inner.X
	}
	left, okLeft := soleComparison(first)
	right, okRight := soleComparison(cmd.Y)
	if !okLeft || !okRight {
		return
	}
	left, okLeft = constantOnRight(left)
	right, okRight = constantOnRight(right)
	if !okLeft || !okRight {
		return
	}
	at := posSpan(cmd.OpPos, 2)
	if cmd.Op == syntax.AndStmt {
		if p.alwaysFalseAnd(left, right) {
			p.report(2333, at, "Both tests compare "+p.nodeText(left.left)+" with different constants, so they can never succeed together; you probably meant ||.")
		}
		return
	}
	if p.alwaysTrueOr(left, right) {
		p.report(2252, at, p.nodeText(left.left)+" always differs from at least one of these constants, so this || chain is always true; you probably meant &&.")
	}
}

func soleComparison(stmt *syntax.Stmt) (comparison, bool) {
	if stmt.Negated || stmt.Background || stmt.Coprocess {
		return comparison{}, false
	}
	t, ok := testCommandOf(stmt.Cmd)
	if !ok || !t.bracketed() || t.unterminated || t.malformed() {
		return comparison{}, false
	}
	return comparisonOf(t.expr)
}

func constantOnRight(c comparison) (comparison, bool) {
	leftConstant, rightConstant := isConstantWord(c.left, true), isConstantWord(c.right, true)
	switch {
	case leftConstant == rightConstant:
		return comparison{}, false
	case leftConstant:
		c.left, c.right = c.right, c.left
	}
	return c, true
}

func (p *pass) checkContradiction(and, doubleBracket bool, at parser.Span, left, right comparison) {
	switch {
	case and && p.alwaysFalseAnd(left, right):
		alternative := "-o"
		if doubleBracket {
			alternative = "||"
		}
		p.report(2333, at, p.nodeText(left.left)+" cannot equal two different values at once, so this condition is always false; you probably meant "+alternative+".")
	case !and && p.alwaysTrueOr(left, right):
		alternative := "-a"
		if doubleBracket {
			alternative = "&&"
		}
		p.report(2055, at, p.nodeText(left.left)+" always differs from at least one of these values, so this condition is always true; you probably meant "+alternative+".")
	}
}

func (p *pass) alwaysTrueOr(a, b comparison) bool {
	return a.op == b.op && (a.op == "-ne" || a.op == "!=") &&
		p.sameText(a.left, b.left) && !p.sameText(a.right, b.right) &&
		!isGlob(a.right) && !isGlob(b.right)
}

func (p *pass) alwaysFalseAnd(a, b comparison) bool {
	if a.op != b.op || !p.sameText(a.left, b.left) || p.sameText(a.right, b.right) {
		return false
	}
	switch a.op {
	case "-eq":
		return isDigitWord(a.right) && isDigitWord(b.right)
	case "=", "==":
		return isPlainLiteral(a.right) && isPlainLiteral(b.right)
	}
	return false
}

func isPlainLiteral(w *syntax.Word) bool {
	_, ok := parser.Literal(w)
	return ok && !isGlob(w)
}

func isDigitWord(w *syntax.Word) bool {
	lit, ok := parser.Literal(w)
	return ok && parser.IsDigits(lit)
}

func (p *pass) sameText(a, b syntax.Node) bool {
	return p.nodeText(a) == p.nodeText(b)
}

func checkArithmeticContradiction(p *pass, n syntax.Node) {
	bin, ok := n.(*syntax.BinaryArithm)
	if !ok || (bin.Op != syntax.OrArit && bin.Op != syntax.AndArit) {
		return
	}
	left, okLeft := bin.X.(*syntax.BinaryArithm)
	right, okRight := bin.Y.(*syntax.BinaryArithm)
	if !okLeft || !okRight || left.Op != right.Op || !p.sameText(left.X, right.X) {
		return
	}
	at := posSpan(bin.OpPos, 2)
	subject := p.nodeText(left.X)
	switch {
	case bin.Op == syntax.OrArit && left.Op == syntax.Neq:
		p.report(2056, at, subject+" always differs from at least one of these values, so this || is always true; you probably meant &&.")
	case bin.Op == syntax.AndArit && left.Op == syntax.Eql && isDigitArithm(left.Y) && isDigitArithm(right.Y):
		p.report(2334, at, subject+" cannot equal two numbers at once, so this && is always false; you probably meant ||.")
	}
}

func isDigitArithm(expr syntax.ArithmExpr) bool {
	w, ok := expr.(*syntax.Word)
	return ok && parser.IsDigits(w.Lit())
}

func checkTestRedirect(p *pass, _ command) {
	stmt, ok := p.parent().(*syntax.Stmt)
	if !ok {
		return
	}
	for _, r := range stmt.Redirs {
		if (r.Op != syntax.RdrOut && r.Op != syntax.RdrIn) || (r.N != nil && r.N.Value == "2") {
			continue
		}
		span, ok := p.span(r)
		if !ok {
			continue
		}
		p.report(2065, span, "This "+r.Op.String()+" is a file redirection, not a comparison, so test never sees it; escape it as \\"+r.Op.String()+" or use [[ ]].")
	}
}

func checkRejectedUnknownOperators(p *pass) {
	e, ok := p.rootParseError()
	if !ok || !p.insideDoubleBracket(e.Span.Start) {
		return
	}
	token := p.text[e.Span.Start:e.Span.End]
	if isOperatorShaped(token) && testOperators[token].kind == notTestOperator {
		p.report(2057, e.Span, token+" is not a comparison operator that [[ ]] understands; check the spelling (see help test).")
		return
	}
	start := lineStart(p.text, e.Span.Start)
	fields := strings.Fields(p.text[start:e.Span.Start])
	if len(fields) == 0 {
		return
	}
	previous := fields[len(fields)-1]
	if !isOperatorShaped(previous) || isUnaryTestOperator(previous) {
		return
	}
	at := strings.LastIndex(p.text[start:e.Span.Start], previous) + start
	p.report(2058, parser.Span{Start: at, End: at + len(previous)}, previous+" is not a test operator, so [[ ]] cannot evaluate this condition; check the spelling (see help test).")
}
