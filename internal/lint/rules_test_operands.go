package lint

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var testOperandsFamily = family{
	codes: map[Code]meta{
		2143: {severity: SeverityStyle, summary: "A test checks whether grep's output is empty instead of using its exit status."},
		2144: {severity: SeverityError, summary: "A file test operator is given a glob, which it does not expand into a list."},
		2171: {severity: SeverityWarning, summary: "A closing bracket ends a test that was never opened."},
		2181: {severity: SeverityStyle, summary: "$? is inspected after a command instead of testing the command directly."},
		2198: {severity: SeverityWarning, summary: "[ ] receives each element of an array as a separate word, which breaks the test."},
		2199: {severity: SeverityError, summary: "[[ ]] joins an array expansion into one string."},
		2200: {severity: SeverityWarning, summary: "Brace expansion turns a [ ] operand into several words."},
		2201: {severity: SeverityError, summary: "Brace expansion does not happen inside [[ ]]."},
		2202: {severity: SeverityWarning, summary: "A glob in a [ ] operand can expand to several words."},
		2203: {severity: SeverityError, summary: "[[ ]] does not expand a glob outside the right side of = and !=."},
		2204: {severity: SeverityError, summary: "( ) runs a subshell, not a test."},
		2205: {severity: SeverityWarning, summary: "( ) runs a subshell, not a comparison."},
		2208: {severity: SeverityError, summary: "The variable name given to -v is expanded as a glob."},
		2245: {severity: SeverityWarning, summary: "ksh applies a file test operator only to the first match of a glob."},
		2255: {severity: SeverityError, summary: "A numeric operand in [ ] is an unquoted glob, not a number."},
	},
	nodes: []nodeRule{
		onNodes(checkTestOperands, &syntax.CallExpr{}, &syntax.TestClause{}),
		onNodes(checkStatusComparison, &syntax.BinaryTest{}, &syntax.CallExpr{}, &syntax.BinaryArithm{}, &syntax.UnaryArithm{}, &syntax.ArithmCmd{}),
		onNode(checkStrayBracket),
		onNode(checkSubshellAsTest),
	},
}

var kshFirstMatchOperators = setOf(
	"-b", "-c", "-d", "-f", "-g", "-k", "-p", "-r", "-s", "-u", "-w", "-x", "-L", "-h", "-N", "-O", "-G", "-R", "-S",
)

var grepCommands = []string{"grep", "egrep", "fgrep", "bz3grep", "bzgrep", "xzgrep", "zgrep", "zipgrep", "zstdgrep"}

func (p *pass) bracketTail(call *syntax.CallExpr) (testCommand, bool) {
	tail, ok := bracketTailWords(call)
	if !ok || len(tail) == 0 {
		return testCommand{}, false
	}
	previous, ok := p.previousStatement()
	if !ok {
		return testCommand{}, false
	}
	opener, ok := bracketCall(previous.Cmd)
	if !ok || endsWithBracket(opener) {
		return testCommand{}, false
	}
	list, joined := p.ancestor(1).(*syntax.BinaryCmd)
	if joined && list.Op != syntax.AndStmt && list.Op != syntax.OrStmt {
		return testCommand{}, false
	}
	words := slices.Clone(opener.Args[1:])
	if joined {
		words = append(words, &syntax.Word{Parts: []syntax.WordPart{&syntax.Lit{ValuePos: list.OpPos, Value: testAnd}}})
	}
	return testOf(singleBracket, append(words, tail...)), true
}

func checkTestOperands(p *pass, n syntax.Node) {
	if t, ok := p.operandTest(n); ok {
		p.testOperands(t, p.checkTestOperand)
	}
}

func (p *pass) operandTest(n syntax.Node) (testCommand, bool) {
	cmd, ok := n.(syntax.Command)
	if !ok {
		return testCommand{}, false
	}
	t, ok := testCommandOf(cmd)
	if !ok || t.style == testBuiltin || t.unterminated || !t.complete {
		return testCommand{}, false
	}
	call, isCall := cmd.(*syntax.CallExpr)
	return t, !isCall || !p.redirectInside(call)
}

func (p *pass) checkTestOperand(o testOperand) {
	span, ok := p.span(o.word)
	if !ok {
		return
	}
	glob := isGlob(o.word) && (!o.double || !qualifiedGlob(o.word, false))
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
		kind := testOperators[o.op].kind
		p.arrayOperand(o, span)
		if !o.double || kind != regexTest || o.shape != shapeRight {
			p.braceOperand(o, span)
		}
		switch {
		case !glob:
		case kind == arithmeticTest:
			if !o.double && p.dialect != dialect.Ksh {
				p.report(2255, span, "In [ ], "+p.spanText(span)+" is a glob, not a number; quote it or compare arithmetically with (( )).")
			}
		case o.shape == shapeRight && (kind == equalityTest || kind == regexTest):
		default:
			p.globOperand(o, span)
		}
	}
}

func (p *pass) unaryGlob(o testOperand, span parser.Span) {
	text := p.spanText(span)
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
	text := p.spanText(span)
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
	cs, ok := parser.SolePart(o.word).(*syntax.CmdSubst)
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
	pe, ok := parser.SolePart(w).(*syntax.ParamExp)
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
		t, ok := p.operandTest(n)
		if !ok || p.firstInFunction(n) {
			return
		}
		var pair [2]testOperand
		count := 0
		p.testOperands(t, func(o testOperand) {
			if count < len(pair) {
				pair[count] = o
			}
			count++
		})
		if count == len(pair) && pair[0].binary() {
			p.statusComparison(pair[0].word, pair[1].word, pair[0].op, pair[0].negated)
		}
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

func checkStrayBracket(p *pass, call *syntax.CallExpr) {
	if len(call.Args) == 0 {
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

func checkSubshellAsTest(p *pass, sub *syntax.Subshell) {
	if len(sub.Stmts) != 1 {
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
	if first, _ := parser.Literal(call.Args[0]); first == "!" || isUnaryTestOperator(first) {
		p.report(2204, span, "( ) runs a subshell, not a test; use [ "+first+" ... ] to test.")
	}
	if second, _ := parser.Literal(call.Args[1]); isBinaryTestOperator(second) {
		p.report(2205, span, "( ) runs a subshell, not a test; use [ ... "+second+" ... ] to compare.")
	}
}
