package lint

import (
	"iter"
	"regexp"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

var comparisonsFamily = family{
	codes: map[Code]meta{
		2049: {severity: SeverityWarning},
		2050: {severity: SeverityWarning},
		2070: {severity: SeverityError},
		2071: {severity: SeverityError},
		2072: {severity: SeverityError},
		2073: {severity: SeverityError},
		2074: {severity: SeverityError},
		2075: {severity: SeverityError},
		2076: {severity: SeverityWarning},
		2077: {severity: SeverityError},
		2078: {severity: SeverityError},
		2107: {severity: SeverityError},
		2108: {severity: SeverityError},
		2109: {severity: SeverityError},
		2110: {severity: SeverityError},
		2122: {severity: SeverityError},
		2157: {severity: SeverityError},
		2158: {severity: SeverityError},
		2159: {severity: SeverityError},
		2160: {severity: SeverityStyle},
		2161: {severity: SeverityStyle},
		2166: {severity: SeverityWarning},
		2170: {severity: SeverityError},
		2193: {severity: SeverityWarning},
		2309: {severity: SeverityWarning},
	},
	rules: []rule{
		{node: checkComparisons},
		{node: checkSplitBracket},
		{finish: checkRejectedTestOperators},
	},
}

type testOp struct {
	name       string
	span       parser.Span
	redirected bool
}

func opAt(pos syntax.Pos, name string) testOp {
	return testOp{name: name, span: posSpan(pos, len(name))}
}

func checkComparisons(p *pass, n syntax.Node) {
	cmd, ok := n.(syntax.Command)
	if !ok {
		return
	}
	t, ok := testCommandOf(cmd)
	if !ok || !t.bracketed() || t.unterminated {
		return
	}
	double := t.style == doubleBracket
	if !double {
		if orderings := p.bracketOrderings(t); len(orderings) > 0 {
			for _, o := range orderings {
				p.checkComparison(false, o.op, o.x, o.y)
			}
			return
		}
	}
	if t.malformed {
		return
	}
	walkTest(t.expr, func(e syntax.TestExpr) { p.checkTestNode(double, e) })
}

type ordering struct {
	op   testOp
	x, y *syntax.Word
}

func (p *pass) bracketOrderings(t testCommand) []ordering {
	var out []ordering
	for i := 1; i+1 < len(t.args); i++ {
		if lit, ok := parser.Literal(t.args[i]); ok && (lit == "<=" || lit == ">=") {
			if span, ok := p.span(t.args[i]); ok {
				out = append(out, ordering{op: testOp{name: lit, span: span}, x: t.args[i-1], y: t.args[i+1]})
			}
		}
	}
	stmt, ok := p.parent().(*syntax.Stmt)
	call, isCall := t.node.(*syntax.CallExpr)
	if !ok || !isCall || stmt.Cmd != t.node {
		return out
	}
	closing := parser.Offset(call.Args[len(call.Args)-1].Pos())
	for _, r := range stmt.Redirs {
		if r.N != nil || (r.Op != syntax.RdrIn && r.Op != syntax.RdrOut) || parser.Offset(r.OpPos) > closing {
			continue
		}
		if o, ok := redirectedOrdering(t.args, r); ok {
			out = append(out, o)
		}
	}
	return out
}

func redirectedOrdering(args []*syntax.Word, r *syntax.Redirect) (ordering, bool) {
	op := opAt(r.OpPos, r.Op.String())
	op.redirected = true
	at := parser.Offset(r.OpPos)
	var x, y *syntax.Word
	for _, a := range args {
		switch {
		case parser.Offset(a.End()) <= at:
			x = a
		case y == nil && parser.Offset(a.Pos()) > parser.Offset(r.Word.End()):
			y = a
		}
	}
	if literalIs(r.Word, "=") && parser.Offset(r.Word.Pos()) == op.span.End {
		op.name += "="
		op.span.End++
	} else {
		y = r.Word
	}
	return ordering{op: op, x: x, y: y}, x != nil && y != nil
}

func (p *pass) checkTestNode(double bool, e syntax.TestExpr) {
	switch e := e.(type) {
	case *syntax.Word:
		p.recordEmptinessTest(e)
		p.checkNullaryTest(e)
	case *syntax.UnaryTest:
		operand, ok := e.X.(*syntax.Word)
		if !ok {
			return
		}
		if e.Op == syntax.TsNempStr || e.Op == syntax.TsEmpStr {
			p.recordEmptinessTest(operand)
		}
		p.checkUnaryTest(double, opAt(e.OpPos, e.Op.String()), operand)
	case *syntax.BinaryTest:
		if e.Op == syntax.AndTest || e.Op == syntax.OrTest {
			if !double {
				p.checkLogicalTest(posSpan(e.OpPos, 2), e.Op)
			}
			return
		}
		x, xok := e.X.(*syntax.Word)
		y, yok := e.Y.(*syntax.Word)
		if xok && yok {
			p.checkComparison(double, opAt(e.OpPos, e.Op.String()), x, y)
		}
	}
}

func (p *pass) checkLogicalTest(at parser.Span, op syntax.BinTestOperator) {
	if op == syntax.AndTest {
		p.report(2166, at, "-a inside [ ] is ambiguous and obsolescent; write [ p ] && [ q ] instead.")
		return
	}
	p.report(2166, at, "-o inside [ ] is ambiguous and obsolescent; write [ p ] || [ q ] instead.")
}

var constantTestCodes = map[string]Code{"false": 2158, "0": 2159, "true": 2160, "1": 2161}

func (p *pass) checkNullaryTest(w *syntax.Word) {
	span, ok := p.span(w)
	if !ok {
		return
	}
	if lit, ok := parser.Literal(w); ok && isUnaryTestOperator(lit) {
		return
	}
	if isConstantWord(w, false) {
		text, _ := parser.Literal(w)
		switch code := constantTestCodes[text]; code {
		case 2158, 2159:
			p.report(code, span, "[ "+text+" ] only tests that the string \""+text+"\" is non-empty, so it is always true; run the "+text+" command without brackets, or compare explicitly.")
		case 2160:
			p.report(code, span, "[ true ] is the same as running true; drop the brackets.")
		case 2161:
			p.report(code, span, "[ 1 ] is always true; write true instead.")
		default:
			p.report(2078, span, "This test only checks that the constant string "+strings.TrimSpace(p.text[span.Start:span.End])+" is non-empty, so it is always true; did you mean to expand a variable or run a command?")
		}
		return
	}
	if at, ok := p.equalsWithoutSpaces(w); ok {
		p.report(2077, at, "A comparison needs spaces around its operator; as one word this only tests that the string is non-empty.")
		return
	}
	if at, ok := p.firstLiteralText(w); ok {
		p.report(2157, at, "This word always contains the literal text here, so the implicit non-empty test is always true; did you mean to compare it with something?")
	}
}

func (p *pass) equalsWithoutSpaces(w *syntax.Word) (parser.Span, bool) {
	for _, part := range w.Parts {
		var text string
		switch part := part.(type) {
		case *syntax.Lit:
			text = part.Value
		case *syntax.SglQuoted:
			text = part.Value
		case *syntax.DblQuoted:
			text = literalText(&syntax.Word{Parts: []syntax.WordPart{part}})
		}
		eq := strings.IndexByte(text, '=')
		if eq < 0 {
			continue
		}
		span, ok := p.span(part)
		if !ok {
			return parser.Span{}, false
		}
		if _, isLit := part.(*syntax.Lit); isLit {
			span.Start += strings.LastIndexByte(text[:eq], '!') + 1
		}
		return span, true
	}
	return parser.Span{}, false
}

func (p *pass) firstLiteralText(w *syntax.Word) (parser.Span, bool) {
	for _, part := range w.Parts {
		switch part := part.(type) {
		case *syntax.Lit:
			if runs := literalRuns(part.Value); len(runs) > 0 {
				return p.within(part, runs[0])
			}
		case *syntax.SglQuoted:
			if part.Value != "" {
				return p.span(part)
			}
		case *syntax.DblQuoted:
			for _, inner := range part.Parts {
				if lit, ok := inner.(*syntax.Lit); ok && lit.Value != "" {
					return p.span(lit)
				}
			}
		}
	}
	return parser.Span{}, false
}

func (p *pass) within(lit *syntax.Lit, run parser.Span) (parser.Span, bool) {
	span, ok := p.span(lit)
	return parser.Span{Start: span.Start + run.Start, End: span.Start + run.End}, ok
}

func literalRuns(value string) []parser.Span {
	var runs []parser.Span
	start := 0
	cut := func(from, to int) {
		if from > start {
			runs = append(runs, parser.Span{Start: start, End: from})
		}
		start = to
	}
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '\\':
			i++
		case '*', '?':
			cut(i, i+1)
		case '[':
			if end := bracketEnd(value, i); end > 0 {
				cut(i, end+1)
				i = end
			}
		case '{':
			if end := braceEnd(value, i); end > 0 {
				cut(i, end+1)
				i = end
			}
		}
	}
	cut(len(value), len(value))
	return runs
}

func braceEnd(value string, open int) int {
	depth := 0
	for i := open; i < len(value); i++ {
		switch value[i] {
		case '\\':
			i++
		case '{':
			depth++
		case '}':
			depth--
			if depth > 0 {
				continue
			}
			inner := value[open+1 : i]
			if strings.Contains(inner, ",") || strings.Contains(inner, "..") {
				return i
			}
			return -1
		}
	}
	return -1
}

func unquotedParts(w *syntax.Word) iter.Seq[syntax.WordPart] {
	return func(yield func(syntax.WordPart) bool) {
		for _, part := range w.Parts {
			dq, ok := part.(*syntax.DblQuoted)
			if !ok {
				if !yield(part) {
					return
				}
				continue
			}
			for _, inner := range dq.Parts {
				if !yield(inner) {
					return
				}
			}
		}
	}
}

func (p *pass) checkUnaryTest(double bool, op testOp, w *syntax.Word) {
	if !double && op.name == "-n" && willSplit(w) && !hasArrayExpansion(w) {
		if span, ok := p.span(w); ok {
			p.report(2070, span, "-n with an unquoted operand inside [ ] is true even when the value is empty; quote the operand or use [[ ]].")
		}
	}
	if op.name != "-n" && op.name != "-z" {
		return
	}
	at, ok := p.firstLiteralText(w)
	if !ok {
		return
	}
	if op.name == "-n" {
		p.report(2157, at, "The operand of -n contains literal text, so it is never empty and the test is always true.")
		return
	}
	p.report(2157, at, "The operand of -z contains literal text, so it is never empty and the test is always false.")
}

func willSplit(w *syntax.Word) bool {
	for _, part := range w.Parts {
		switch part := part.(type) {
		case *syntax.ParamExp, *syntax.CmdSubst, *syntax.BraceExp, *syntax.ExtGlob:
			return true
		case *syntax.DblQuoted:
			if hasArrayExpansion(&syntax.Word{Parts: part.Parts}) {
				return true
			}
		}
	}
	return isGlob(w)
}

func hasArrayExpansion(w *syntax.Word) bool {
	for part := range unquotedParts(w) {
		if pe, ok := part.(*syntax.ParamExp); ok && isArrayExpansion(pe) {
			return true
		}
	}
	return false
}

func isArrayExpansion(pe *syntax.ParamExp) bool {
	return !pe.Length && kindOf(pe) == expandsElements && !isAlternativeExpansion(pe)
}

func isConstantWord(w *syntax.Word, regex bool) bool {
	if !regex && isPattern(w) {
		return false
	}
	for i, part := range w.Parts {
		switch part := part.(type) {
		case *syntax.Lit:
			if i == 0 && strings.HasPrefix(part.Value, "~") {
				return false
			}
		case *syntax.SglQuoted:
		case *syntax.DblQuoted:
			for _, inner := range part.Parts {
				if _, ok := inner.(*syntax.Lit); !ok {
					return false
				}
			}
		default:
			return false
		}
	}
	return true
}

var (
	orderingOps       = setOf("<", ">", "<=", ">=")
	strictOrder       = setOf("<", ">")
	dynamicTests      = setOf("-nt", "-ot", "-ef")
	arithmeticTestOps = setOf("-eq", "-ne", "-lt", "-le", "-gt", "-ge")
	equalityTests     = setOf("=", "==", "!=")
	decimalNumber     = regexp.MustCompile(`^[+-]?[0-9]+\.[0-9]+$`)
)

func (p *pass) checkComparison(double bool, op testOp, x, y *syntax.Word) {
	if op.name == "=~" && !double && p.dialect.Features().DoubleBracket {
		p.report(2074, op.span, "=~ only works inside [[ ]]; [ ] has no regex matching.")
	}
	switch {
	case orderingOps[op.name]:
		p.checkOrdering(double, op, x, y)
	case arithmeticTestOps[op.name]:
		p.checkArithmeticOperands(double, op, x, y)
	}
	if double && op.name == "=~" {
		p.checkRegexOperand(y)
	}
	if (double && arithmeticTestOps[op.name]) || dynamicTests[op.name] {
		return
	}
	if isConstantWord(x, false) && isConstantWord(y, op.name == "=~") {
		p.report(2050, op.span, "Both sides of "+op.name+" are constant, so this test always has the same result; did you forget a $ on a variable?")
		return
	}
	if equalityTests[op.name] && !overlaps(globOf(x), globOf(y)) {
		p.report(2193, op.span, "No string can match both sides of "+op.name+", so this comparison never succeeds.")
	}
}

func (p *pass) checkOrdering(double bool, op testOp, x, y *syntax.Word) {
	posix := p.dialect == dialect.Posix
	if (strictOrder[op.name] || !posix) && (p.isNumericOperand(x) || p.isNumericOperand(y)) {
		p.report(2071, op.span, op.name+" compares strings, not numbers; use "+numericOperator[op.name]+" (or (( )) arithmetic) for numbers.")
		return
	}
	p.checkDecimals(x, y)
	if !posix && !strictOrder[op.name] {
		p.report(2122, op.span, op.name+" is not a valid test operator; negate the opposite comparison, e.g. ! [ a "+escapedOpposite(double, op.name)+" b ].")
	}
	if op.redirected && strictOrder[op.name] && !posix {
		hint := ""
		if p.dialect.Features().DoubleBracket {
			hint = " or use [[ ]]"
		}
		p.report(2073, op.span, "An unescaped "+op.name+" inside [ ] is a redirection, not a comparison; escape it as \\"+op.name+hint+".")
	}
}

var numericOperator = map[string]string{"<": "-lt", ">": "-gt", "<=": "-le", ">=": "-ge"}

func escapedOpposite(double bool, op string) string {
	opposite := map[string]string{"<=": ">", ">=": "<"}[op]
	if double {
		return opposite
	}
	return `\` + opposite
}

func (p *pass) checkDecimals(words ...*syntax.Word) {
	if p.dialect == dialect.Ksh {
		return
	}
	for _, w := range words {
		if !decimalNumber.MatchString(roughText(w)) {
			continue
		}
		if span, ok := p.span(w); ok {
			p.report(2072, span, "Shell tests compare integers only; "+roughText(w)+" is a decimal, so compare it with bc or awk instead.")
		}
	}
}

func (p *pass) checkArithmeticOperands(double bool, op testOp, x, y *syntax.Word) {
	p.checkDecimals(x, y)
	for _, w := range []*syntax.Word{x, y} {
		if isNumericText(literalText(w)) {
			continue
		}
		span, ok := p.span(w)
		if !ok {
			continue
		}
		switch {
		case !double:
			p.report(2170, span, op.name+" compares numbers, but this operand is not a number; use "+stringOperator[op.name]+" to compare strings.")
		case p.namesUnassignedVariable(w):
			p.report(2309, span, op.name+" inside [[ ]] evaluates this operand as an arithmetic expression or variable name; use "+stringOperator[op.name]+" to compare strings, or $ to expand a variable.")
		}
	}
}

var stringOperator = map[string]string{"-eq": "=", "-ne": "!=", "-lt": `\<`, "-le": `\< or =`, "-gt": `\>`, "-ge": `\> or =`}

func (p *pass) namesUnassignedVariable(w *syntax.Word) bool {
	name := roughText(w)
	if !parser.IsName(name) || hasQuotes(w) {
		return true
	}
	span, _ := p.span(w)
	return p.tree.Visible(name, p.tree.ScopeAt(span.Start)).Len() == 0
}

func hasQuotes(w *syntax.Word) bool {
	for _, part := range w.Parts {
		switch part.(type) {
		case *syntax.SglQuoted, *syntax.DblQuoted:
			return true
		}
	}
	return false
}

func isNumericText(s string) bool {
	return strings.Trim(s, "0123456789+-. ") == ""
}

func (p *pass) isNumericOperand(w *syntax.Word) bool {
	switch part := solePart(w).(type) {
	case *syntax.ArithmExp:
		return true
	case *syntax.ParamExp:
		return p.flow.mayHoldInteger(part)
	}
	text := roughText(w)
	return text != "" && strings.Trim(text, "0123456789") == ""
}

var regexMetacharacters = "[]*.+()|"

func (p *pass) checkRegexOperand(w *syntax.Word) {
	span, ok := p.span(w)
	if !ok {
		return
	}
	if len(w.Parts) == 1 && quotesRegex(w.Parts[0]) {
		p.report(2076, span, "Quoting the right-hand side of =~ makes it match literally; leave it unquoted (or store the regex in a variable) to match as a regex.")
	}
	text := regexText(w)
	if strings.HasPrefix(text, "*") || (len(text) == 2 && text[1] == '*' && text[0] != '\\' && text[0] != '.') {
		p.report(2049, span, "=~ takes a regex, not a glob; use == for glob patterns or write the regex as .* style.")
	}
}

func quotesRegex(part syntax.WordPart) bool {
	switch part := part.(type) {
	case *syntax.SglQuoted:
		return strings.ContainsAny(part.Value, regexMetacharacters)
	case *syntax.DblQuoted:
		for _, inner := range part.Parts {
			lit, ok := inner.(*syntax.Lit)
			if !ok || strings.ContainsAny(lit.Value, regexMetacharacters) {
				return true
			}
		}
	}
	return false
}

const expansionPlaceholder = "${VAR}"

func roughText(w *syntax.Word) string {
	text, _ := parser.Text(w, expansionPlaceholder)
	return text
}

func regexText(w *syntax.Word) string {
	var sb strings.Builder
	for part := range unquotedParts(w) {
		switch part := part.(type) {
		case *syntax.Lit:
			sb.WriteString(part.Value)
		case *syntax.SglQuoted:
			sb.WriteString(part.Value)
		default:
			sb.WriteString(expansionPlaceholder)
		}
	}
	return sb.String()
}

func literalText(w *syntax.Word) string {
	var sb strings.Builder
	for _, part := range w.Parts {
		switch part := part.(type) {
		case *syntax.Lit:
			for _, run := range literalRuns(part.Value) {
				sb.WriteString(part.Value[run.Start:run.End])
			}
		case *syntax.SglQuoted:
			sb.WriteString(part.Value)
		case *syntax.DblQuoted:
			for _, inner := range part.Parts {
				if lit, ok := inner.(*syntax.Lit); ok {
					sb.WriteString(lit.Value)
				}
			}
		}
	}
	return sb.String()
}

func checkSplitBracket(p *pass, n syntax.Node) {
	bin, ok := n.(*syntax.BinaryCmd)
	if !ok || (bin.Op != syntax.AndStmt && bin.Op != syntax.OrStmt) {
		return
	}
	left, ok := bin.X.Cmd.(*syntax.CallExpr)
	if !ok || len(left.Args) == 0 || left.Args[0].Lit() != "[" || left.Args[len(left.Args)-1].Lit() == "]" {
		return
	}
	right, ok := bin.Y.Cmd.(*syntax.CallExpr)
	if !ok || len(right.Args) == 0 || right.Args[0].Lit() == "[" || right.Args[len(right.Args)-1].Lit() != "]" {
		return
	}
	op := opAt(bin.OpPos, bin.Op.String())
	if bin.Op == syntax.AndStmt {
		p.report(2107, op.span, "&& cannot be used inside [ ]; write [ p ] && [ q ] or use [[ p && q ]].")
		return
	}
	p.report(2109, op.span, "|| cannot be used inside [ ]; write [ p ] || [ q ] or use [[ p || q ]].")
}

func checkRejectedTestOperators(p *pass) {
	e, ok := p.rootParseError()
	if !ok || !p.insideDoubleBracket(e.Span.Start) {
		return
	}
	switch p.text[e.Span.Start:e.Span.End] {
	case "-a":
		p.report(2108, e.Span, "-a does not combine tests inside [[ ]]; use && instead.")
	case "-o":
		p.report(2110, e.Span, "-o does not combine tests inside [[ ]]; use || instead.")
	case `\<`, `\>`, `"<"`, `">"`, `'<'`, `'>'`:
		p.report(2075, e.Span, "Inside [[ ]] the comparison operators < and > must not be escaped or quoted.")
	}
}

func (p *pass) insideDoubleBracket(offset int) bool {
	line := p.text[strings.LastIndexByte(p.text[:offset], '\n')+1 : offset]
	return strings.LastIndex(line, "[[") > strings.LastIndex(line, "]]")
}

func isPattern(w *syntax.Word) bool { return isGlob(w) || hasBraceExpansion(w) }

func isUnaryTestOperator(s string) bool {
	_, ok := unaryTestOps[s]
	return ok
}
