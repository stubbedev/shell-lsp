package lint

import (
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

var testOperandsFamily = family{
	codes: map[Code]meta{
		2143: {severity: SeverityStyle},
		2144: {severity: SeverityError},
		2171: {severity: SeverityWarning},
		2181: {severity: SeverityStyle},
		2198: {severity: SeverityWarning},
		2199: {severity: SeverityError},
		2200: {severity: SeverityWarning},
		2201: {severity: SeverityError},
		2202: {severity: SeverityWarning},
		2203: {severity: SeverityError},
		2204: {severity: SeverityError},
		2205: {severity: SeverityWarning},
		2208: {severity: SeverityError},
		2245: {severity: SeverityWarning},
		2255: {severity: SeverityError},
	},
	rules: []rule{
		{node: checkTestOperands},
		{node: checkStatusComparison},
		{node: checkStrayBracket},
		{node: checkSubshellAsTest},
	},
}

var arithmeticTestOperators = setOf("-eq", "-ne", "-lt", "-le", "-gt", "-ge")

var stringTestOperators = setOf("=", "==", "!=", "=~")

var kshFirstMatchOperators = setOf(
	"-b", "-c", "-d", "-f", "-g", "-k", "-p", "-r", "-s", "-u", "-w", "-x", "-L", "-h", "-N", "-O", "-G", "-R", "-S",
)

var grepCommands = []string{"grep", "egrep", "fgrep", "bz3grep", "bzgrep", "xzgrep", "zgrep", "zipgrep", "zstdgrep"}

type operandShape uint8

const (
	shapeNullary operandShape = iota
	shapeUnary
	shapeLeft
	shapeRight
)

type testOperand struct {
	word     *syntax.Word
	op       string
	opWord   *syntax.Word
	negation *syntax.Word
	shape    operandShape
	double   bool
	start    int
}

func (o testOperand) binary() bool { return o.shape == shapeLeft || o.shape == shapeRight }

func isBinaryTestOperator(op string) bool {
	return binaryTestOperators[op] || (isDashOperator(op) && op != "-a" && op != "-o")
}

func isDashOperator(op string) bool {
	for _, dash := range []string{"-", "–", "—"} {
		if rest, found := strings.CutPrefix(op, dash); found && rest != "" {
			return true
		}
	}
	return false
}

func (p *pass) bracketOperandWords(call *syntax.CallExpr) ([]*syntax.Word, bool, bool) {
	if len(call.Args) == 0 || call.Args[0].Lit() != "[" || p.redirectInside(call) {
		return nil, false, false
	}
	words := call.Args[1:]
	if n := len(words); n > 0 && words[n-1].Lit() == "]" {
		return words[:n-1], true, true
	}
	return words, false, true
}

func (p *pass) redirectInside(call *syntax.CallExpr) bool {
	stmt, ok := p.parent().(*syntax.Stmt)
	if !ok {
		return false
	}
	end := call.End().Offset()
	return slices.ContainsFunc(stmt.Redirs, func(r *syntax.Redirect) bool { return r.OpPos.Offset() < end })
}

func (p *pass) bracketTail(call *syntax.CallExpr) ([]*syntax.Word, bool) {
	if len(call.Args) < 2 || call.Args[len(call.Args)-1].Lit() != "]" || call.Args[0].Lit() == "[" {
		return nil, false
	}
	previous, ok := p.previousStatement()
	if !ok {
		return nil, false
	}
	list, joined := p.ancestor(1).(*syntax.BinaryCmd)
	if joined && list.Op != syntax.AndStmt && list.Op != syntax.OrStmt {
		return nil, false
	}
	opener, ok := previous.Cmd.(*syntax.CallExpr)
	if !ok || len(opener.Args) == 0 || opener.Args[0].Lit() != "[" || opener.Args[len(opener.Args)-1].Lit() == "]" {
		return nil, false
	}
	words := slices.Clone(opener.Args[1:])
	if joined {
		words = append(words, &syntax.Word{Parts: []syntax.WordPart{&syntax.Lit{Value: "-a"}}})
	}
	return append(words, call.Args[:len(call.Args)-1]...), true
}

type bracketParser struct {
	words    []*syntax.Word
	names    []string
	i        int
	operands []testOperand
	joined   bool
}

func parseBracketTest(words []*syntax.Word) (bracketParser, bool) {
	t := bracketParser{words: words, names: make([]string, len(words))}
	for i, w := range words {
		t.names[i], _ = parser.Literal(w)
	}
	ok := true
	for t.i < len(words) {
		start := t.i
		if !t.expression() || t.i < len(words) {
			ok = false
		}
		if t.i == start || t.i < len(words) {
			t.i++
		}
	}
	return t, ok
}

func (t *bracketParser) literal(offset int) string {
	if t.i+offset >= len(t.names) {
		return ""
	}
	return t.names[t.i+offset]
}

func (t *bracketParser) expression() bool {
	if !t.conjunction() {
		return false
	}
	for t.literal(0) == "-o" {
		t.i++
		t.joined = true
		if !t.conjunction() {
			return false
		}
	}
	return true
}

func (t *bracketParser) conjunction() bool {
	if !t.negation(nil) {
		return false
	}
	for t.literal(0) == "-a" {
		t.i++
		t.joined = true
		if !t.negation(nil) {
			return false
		}
	}
	return true
}

func (t *bracketParser) negation(bang *syntax.Word) bool {
	if t.literal(0) == "!" && t.i+1 < len(t.words) {
		bang = t.words[t.i]
		t.i++
		return t.negation(bang)
	}
	return t.primary(bang)
}

func (t *bracketParser) primary(bang *syntax.Word) bool {
	if t.i >= len(t.words) {
		return false
	}
	start := parser.Offset(t.words[t.i].Pos())
	if t.literal(0) == "(" {
		t.i++
		if !t.expression() || t.literal(0) != ")" {
			return false
		}
		t.i++
		return true
	}
	if op := t.literal(1); t.i+2 < len(t.words) && isBinaryTestOperator(op) {
		operator := t.words[t.i+1]
		t.operands = append(t.operands,
			testOperand{word: t.words[t.i], op: op, opWord: operator, shape: shapeLeft, start: start},
			testOperand{word: t.words[t.i+2], op: op, opWord: operator, shape: shapeRight, start: start})
		t.i += 3
		return true
	}
	if op := t.literal(0); isDashOperator(op) && t.i+1 < len(t.words) {
		t.operands = append(t.operands, testOperand{word: t.words[t.i+1], op: op, opWord: t.words[t.i], negation: bang, shape: shapeUnary, start: start})
		t.i += 2
		return true
	}
	t.operands = append(t.operands, testOperand{word: t.words[t.i], shape: shapeNullary, start: start})
	t.i++
	return true
}

func doubleBracketOperands(n syntax.Node, visit func(testOperand)) {
	nullary := func(x syntax.TestExpr) {
		if w, ok := x.(*syntax.Word); ok {
			visit(testOperand{word: w, shape: shapeNullary, double: true, start: parser.Offset(w.Pos())})
		}
	}
	switch n := n.(type) {
	case *syntax.TestClause:
		nullary(n.X)
	case *syntax.ParenTest:
		nullary(n.X)
	case *syntax.UnaryTest:
		w, ok := n.X.(*syntax.Word)
		switch {
		case !ok:
		case n.Op == syntax.TsNot:
			nullary(w)
		default:
			visit(testOperand{word: w, op: n.Op.String(), shape: shapeUnary, double: true, start: parser.Offset(n.OpPos)})
		}
	case *syntax.BinaryTest:
		if n.Op == syntax.AndTest || n.Op == syntax.OrTest {
			nullary(n.X)
			nullary(n.Y)
			return
		}
		start := parser.Offset(n.Pos())
		if w, ok := n.X.(*syntax.Word); ok {
			visit(testOperand{word: w, op: n.Op.String(), shape: shapeLeft, double: true, start: start})
		}
		if w, ok := n.Y.(*syntax.Word); ok {
			visit(testOperand{word: w, op: n.Op.String(), shape: shapeRight, double: true, start: start})
		}
	}
}

func checkTestOperands(p *pass, n syntax.Node) {
	call, ok := n.(*syntax.CallExpr)
	if !ok {
		doubleBracketOperands(n, p.checkTestOperand)
		return
	}
	words, closed, bracket := p.bracketOperandWords(call)
	if !bracket || !closed {
		return
	}
	t, ok := parseBracketTest(words)
	if !ok {
		return
	}
	for _, o := range t.operands {
		p.checkTestOperand(o)
	}
}

func (p *pass) checkTestOperand(o testOperand) {
	span, ok := p.span(o.word)
	if !ok {
		return
	}
	glob := isGlob(o.word)
	switch o.shape {
	case shapeUnary:
		if glob {
			p.unaryGlob(o, span)
			return
		}
		p.arrayOperand(o, span)
		p.braceOperand(o, span)
		p.grepOutputTest(o, span)
	case shapeNullary:
		if o.double {
			p.arrayOperand(o, span)
		}
		p.braceOperand(o, span)
		if glob {
			p.globOperand(o, span)
		}
		p.grepOutputTest(o, span)
	default:
		p.arrayOperand(o, span)
		if !o.double || o.op != "=~" || o.shape != shapeRight {
			p.braceOperand(o, span)
		}
		switch {
		case !glob:
		case arithmeticTestOperators[o.op]:
			if !o.double && p.dialect != dialect.Ksh {
				p.report(2255, span, "In [ ], "+p.text[span.Start:span.End]+" is a glob, not a number; quote it or compare arithmetically with (( )).")
			}
		case stringTestOperators[o.op] && o.shape == shapeRight:
		default:
			p.globOperand(o, span)
		}
	}
}

func (p *pass) unaryGlob(o testOperand, span parser.Span) {
	text := p.text[span.Start:span.End]
	switch {
	case o.op == "-v":
		if !o.double {
			p.report(2208, span, text+" is expanded as a glob here; quote the variable name given to -v.")
		}
	case o.double || p.dialect != dialect.Ksh:
		p.report(2144, span, o.op+" does not work with globs; loop over the matches of "+text+" and test each one.")
	case kshFirstMatchOperators[o.op]:
		p.report(2245, span, "ksh only applies "+o.op+" to the first match of "+text+"; loop over the matches to test them all.")
	}
}

func (p *pass) arrayOperand(o testOperand, span parser.Span) {
	if !hasElementsExpansion(o.word) {
		return
	}
	if o.double {
		p.report(2199, span, "[[ ]] joins this array expansion into one string; loop over the elements, or use ${array[*]} if joining is intended.")
		return
	}
	p.report(2198, span, "[ ] receives each element of this array as a separate word, which breaks the test; loop over the elements or join them with ${array[*]}.")
}

func hasElementsExpansion(w *syntax.Word) bool {
	for _, part := range w.Parts {
		if dq, ok := part.(*syntax.DblQuoted); ok && slices.ContainsFunc(dq.Parts, expandsAllElements) {
			return true
		}
		if expandsAllElements(part) {
			return true
		}
	}
	return false
}

func expandsAllElements(part syntax.WordPart) bool {
	pe, ok := part.(*syntax.ParamExp)
	return ok && kindOf(pe) == expandsElements
}

func (p *pass) braceOperand(o testOperand, span parser.Span) {
	if !hasBraceExpansion(o.word) {
		return
	}
	if o.double {
		p.report(2201, span, "Brace expansion does not happen inside [[ ]]; compare against each alternative or use a pattern.")
		return
	}
	p.report(2200, span, "Brace expansion turns this operand into several words, which breaks [ ]; compare against each alternative separately.")
}

func (p *pass) globOperand(o testOperand, span parser.Span) {
	text := p.text[span.Start:span.End]
	if o.double {
		p.report(2203, span, "[[ ]] does not expand the glob "+text+" here; only the right side of = and != is a pattern.")
		return
	}
	p.report(2202, span, "The glob "+text+" can expand to several words and break [ ]; loop over the matches instead.")
}

func (p *pass) grepOutputTest(o testOperand, span parser.Span) {
	if o.op != "" && o.op != "-n" && o.op != "-z" {
		return
	}
	cs, ok := solePart(o.word).(*syntax.CmdSubst)
	if !ok || cs.Backquotes || cs.TempFile || cs.ReplyVar || len(cs.Stmts) != 1 {
		return
	}
	stages := pipelineStages(cs.Stmts[0], nil)
	call, ok := stages[len(stages)-1].Cmd.(*syntax.CallExpr)
	if !ok {
		return
	}
	c, ok := commandOf(call)
	if !ok || !c.isBase(grepCommands...) {
		return
	}
	at := parser.Span{Start: o.start, End: span.End}
	if o.op == "-z" {
		p.report(2143, at, "Test for no match with ! "+c.name+" -q instead of checking that its output is empty.")
		return
	}
	p.report(2143, at, "Test for a match with "+c.name+" -q instead of checking that its output is non-empty.")
}

func isStatusWord(w *syntax.Word) bool {
	pe, ok := solePart(w).(*syntax.ParamExp)
	return ok && pe.Param != nil && pe.Param.Value == "?" && pe.Exp == nil && pe.Index == nil && !pe.Length && !pe.Excl
}

func isZeroWord(w *syntax.Word) bool {
	lit, ok := parser.Literal(w)
	return ok && lit == "0"
}

var failureComparisons = map[string]bool{"-ne": true, "!=": true, "-gt": true, ">": true}

var mirroredFailureComparisons = map[string]bool{"-ne": true, "!=": true, "-lt": true, "<": true}

func checkStatusComparison(p *pass, n syntax.Node) {
	switch n := n.(type) {
	case *syntax.BinaryTest:
		x, xok := n.X.(*syntax.Word)
		y, yok := n.Y.(*syntax.Word)
		if !xok || !yok {
			return
		}
		negated, ok := p.soleTest(0)
		if !ok {
			return
		}
		p.statusComparison(x, y, n.Op.String(), negated)
	case *syntax.CallExpr:
		words, closed, bracket := p.bracketOperandWords(n)
		if !bracket || !closed {
			return
		}
		t, ok := parseBracketTest(words)
		if !ok || t.joined || len(t.operands) != 2 || !t.operands[0].binary() || p.firstInFunction(n) {
			return
		}
		negated := words[0].Lit() == "!"
		p.statusComparison(t.operands[0].word, t.operands[1].word, t.operands[0].op, negated)
	case *syntax.BinaryArithm:
		x, xok := n.X.(*syntax.Word)
		y, yok := n.Y.(*syntax.Word)
		if !xok || !yok || !isComparisonArithm(n.Op) {
			return
		}
		negated, ok := p.soleArithmetic(0)
		if !ok {
			return
		}
		p.statusComparison(x, y, n.Op.String(), negated)
	case *syntax.UnaryArithm:
		w, ok := n.X.(*syntax.Word)
		if !ok || n.Op != syntax.Not || !isStatusWord(w) {
			return
		}
		if negated, ok := p.soleArithmetic(0); ok {
			p.reportStatus(w, negated)
		}
	case *syntax.ArithmCmd:
		x := n.X
		for {
			paren, ok := x.(*syntax.ParenArithm)
			if !ok {
				break
			}
			x = paren.X
		}
		if w, ok := x.(*syntax.Word); ok && isStatusWord(w) && !p.firstInFunction(n) {
			p.reportStatus(w, true)
		}
	}
}

func isComparisonArithm(op syntax.BinAritOperator) bool {
	switch op {
	case syntax.Gtr, syntax.Lss, syntax.Geq, syntax.Leq, syntax.Eql, syntax.Neq:
		return true
	}
	return false
}

func (p *pass) statusComparison(x, y *syntax.Word, op string, negated bool) {
	switch {
	case isStatusWord(x) && isZeroWord(y):
		p.reportStatus(x, failureComparisons[op] != negated)
	case isZeroWord(x) && isStatusWord(y):
		p.reportStatus(y, mirroredFailureComparisons[op] != negated)
	}
}

func (p *pass) reportStatus(w *syntax.Word, failure bool) {
	span, ok := p.span(w)
	if !ok {
		return
	}
	if failure {
		p.report(2181, span, "Test the command directly with 'if ! mycmd;' instead of inspecting $? afterwards.")
		return
	}
	p.report(2181, span, "Test the command directly with 'if mycmd;' instead of inspecting $? afterwards.")
}

func (p *pass) soleTest(depth int) (bool, bool) {
	negated := false
	for ; ; depth++ {
		switch n := p.ancestor(depth).(type) {
		case *syntax.ParenTest:
		case *syntax.UnaryTest:
			if n.Op != syntax.TsNot {
				return false, false
			}
			negated = !negated
		case *syntax.TestClause:
			return negated, !p.firstInFunction(n)
		default:
			return false, false
		}
	}
}

func (p *pass) soleArithmetic(depth int) (bool, bool) {
	negated := false
	for ; ; depth++ {
		switch n := p.ancestor(depth).(type) {
		case *syntax.ParenArithm:
		case *syntax.UnaryArithm:
			if n.Op != syntax.Not {
				return false, false
			}
			negated = !negated
		case *syntax.ArithmCmd:
			return negated, !p.firstInFunction(n)
		default:
			return false, false
		}
	}
}

func (p *pass) firstInFunction(cmd syntax.Command) bool {
	for _, n := range slices.Backward(p.stack) {
		if fn, ok := n.(*syntax.FuncDecl); ok {
			return fn.Body != nil && firstCommand(fn.Body) == cmd
		}
	}
	return false
}

func firstCommand(s *syntax.Stmt) syntax.Command {
	for {
		switch c := s.Cmd.(type) {
		case *syntax.Block:
			if len(c.Stmts) == 0 {
				return c
			}
			s = c.Stmts[0]
		case *syntax.Subshell:
			if len(c.Stmts) == 0 {
				return c
			}
			s = c.Stmts[0]
		case *syntax.BinaryCmd:
			s = c.X
		case *syntax.IfClause:
			if len(c.Cond) == 0 {
				return c
			}
			s = c.Cond[0]
		default:
			return c
		}
	}
}

func checkStrayBracket(p *pass, n syntax.Node) {
	call, ok := n.(*syntax.CallExpr)
	if !ok || len(call.Args) == 0 {
		return
	}
	last := call.Args[len(call.Args)-1]
	if len(last.Parts) != 1 {
		return
	}
	lit, ok := last.Parts[0].(*syntax.Lit)
	if !ok || (lit.Value != "]" && lit.Value != "]]") {
		return
	}
	opener := "[" + lit.Value[1:]
	if _, tail := p.bracketTail(call); tail || slices.ContainsFunc(call.Args, func(w *syntax.Word) bool { return strings.HasPrefix(w.Lit(), "[") }) {
		return
	}
	if span, ok := p.span(last); ok {
		p.report(2171, span, "This "+lit.Value+" closes a test that was never opened; add the missing "+opener+".")
	}
}

func checkSubshellAsTest(p *pass, n syntax.Node) {
	sub, ok := n.(*syntax.Subshell)
	if !ok || len(sub.Stmts) != 1 {
		return
	}
	stmt := sub.Stmts[0]
	for {
		list, ok := stmt.Cmd.(*syntax.BinaryCmd)
		if !ok || (list.Op != syntax.AndStmt && list.Op != syntax.OrStmt) {
			break
		}
		stmt = list.X
	}
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Assigns) > 0 || len(call.Args) < 2 {
		return
	}
	span, ok := p.span(sub)
	if !ok {
		return
	}
	if first, _ := parser.Literal(call.Args[0]); first == "!" || unaryTestOperators[first] {
		p.report(2204, span, "( ) runs a subshell, not a test; use [ "+first+" ... ] to test.")
	}
	if second, _ := parser.Literal(call.Args[1]); binaryTestOperators[second] {
		p.report(2205, span, "( ) runs a subshell, not a test; use [ ... "+second+" ... ] to compare.")
	}
}
