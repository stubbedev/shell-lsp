package lint

import (
	"cmp"
	"path"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type bracketStyle uint8

const (
	singleBracket bracketStyle = iota
	doubleBracket
	testBuiltin
)

type testCommand struct {
	style        bracketStyle
	name         string
	node         syntax.Command
	args         []*syntax.Word
	expr         syntax.TestExpr
	opaque       []opaqueTest
	complete     bool
	unterminated bool
}

type opaqueTest struct {
	op   *syntax.Word
	name string
	x, y *syntax.Word
}

func (o opaqueTest) unary() bool { return o.y == nil }

func (o opaqueTest) anchor() *syntax.Word {
	if o.unary() {
		return o.op
	}
	return o.x
}

func (t testCommand) bracketed() bool { return t.style != testBuiltin }

func (t testCommand) malformed() bool { return !t.complete || len(t.opaque) > 0 }

func testBuiltinOf(call *syntax.CallExpr) (bracketStyle, string, bool) {
	name, ok := parser.CallName(call)
	switch {
	case !ok:
		return 0, "", false
	case name == "[":
		return singleBracket, name, true
	case name == "test" || (path.Base(name) == "test" && path.Dir(name) != "."):
		return testBuiltin, name, true
	}
	return 0, "", false
}

func testCommandOf(cmd syntax.Command) (testCommand, bool) {
	switch cmd := cmd.(type) {
	case *syntax.TestClause:
		return testCommand{style: doubleBracket, name: "[[", node: cmd, expr: cmd.X, complete: true}, true
	case *syntax.CallExpr:
		style, name, ok := testBuiltinOf(cmd)
		if !ok {
			return testCommand{}, false
		}
		args := cmd.Args[1:]
		closed := style != singleBracket || endsWithBracket(cmd)
		if style == singleBracket && closed {
			args = args[:len(args)-1]
		}
		t := testOf(style, args)
		t.name, t.node, t.unterminated = name, cmd, !closed
		return t, true
	}
	return testCommand{}, false
}

func testOf(style bracketStyle, args []*syntax.Word) testCommand {
	tp := testArgParser{words: args}
	expr := tp.or()
	return testCommand{style: style, args: args, expr: expr, opaque: tp.opaque, complete: tp.pos == len(args) && !tp.unclosed}
}

func isTestCommand(stmt *syntax.Stmt) bool {
	switch cmd := stmt.Cmd.(type) {
	case *syntax.TestClause:
		return true
	case *syntax.CallExpr:
		_, _, ok := testBuiltinOf(cmd)
		return ok
	}
	return false
}

func bracketCall(cmd syntax.Command) (*syntax.CallExpr, bool) {
	call, ok := cmd.(*syntax.CallExpr)
	if !ok {
		return nil, false
	}
	style, _, ok := testBuiltinOf(call)
	return call, ok && style == singleBracket
}

func endsWithBracket(call *syntax.CallExpr) bool {
	return len(call.Args) > 0 && call.Args[len(call.Args)-1].Lit() == "]"
}

func isUnterminatedBracket(cmd syntax.Command) bool {
	call, ok := bracketCall(cmd)
	return ok && !endsWithBracket(call)
}

func bracketTailWords(cmd syntax.Command) ([]*syntax.Word, bool) {
	call, ok := cmd.(*syntax.CallExpr)
	if !ok || !endsWithBracket(call) {
		return nil, false
	}
	if _, bracket := bracketCall(call); bracket {
		return nil, false
	}
	return call.Args[:len(call.Args)-1], true
}

func (p *pass) redirectInside(call *syntax.CallExpr) bool {
	stmt, ok := p.parent().(*syntax.Stmt)
	return ok && slices.ContainsFunc(stmt.Redirs, func(r *syntax.Redirect) bool { return isWithin(call, r) })
}

func isWithin(call *syntax.CallExpr, r *syntax.Redirect) bool {
	return r.OpPos.Offset() < call.End().Offset()
}

func isComparisonRedirect(r *syntax.Redirect) bool {
	return r.N == nil && (r.Op == syntax.RdrIn || r.Op == syntax.RdrOut)
}

type testArgParser struct {
	words    []*syntax.Word
	pos      int
	unclosed bool
	opaque   []opaqueTest
}

func (tp *testArgParser) peek(ahead int) string {
	i := tp.pos + ahead
	if i >= len(tp.words) {
		return ""
	}
	lit, _ := parser.Literal(tp.words[i])
	return lit
}

func (tp *testArgParser) remaining() int { return len(tp.words) - tp.pos }

func (tp *testArgParser) take() *syntax.Word {
	w := tp.words[tp.pos]
	tp.pos++
	return w
}

func (tp *testArgParser) or() syntax.TestExpr {
	x := tp.and()
	for tp.remaining() > 1 {
		if tp.peek(0) != testOr {
			break
		}
		op := tp.take()
		x = &syntax.BinaryTest{OpPos: op.Pos(), Op: syntax.OrTest, X: x, Y: tp.and()}
	}
	return x
}

func (tp *testArgParser) and() syntax.TestExpr {
	x := tp.not()
	for tp.remaining() > 1 {
		if tp.peek(0) != testAnd {
			break
		}
		op := tp.take()
		x = &syntax.BinaryTest{OpPos: op.Pos(), Op: syntax.AndTest, X: x, Y: tp.not()}
	}
	return x
}

func (tp *testArgParser) not() syntax.TestExpr {
	if tp.peek(0) == "!" && tp.remaining() > 1 {
		if _, _, binary := tp.binaryAhead(); !binary {
			op := tp.take()
			return &syntax.UnaryTest{OpPos: op.Pos(), Op: syntax.TsNot, X: tp.not()}
		}
	}
	return tp.primary()
}

func (tp *testArgParser) binaryAhead() (string, testOperator, bool) {
	if tp.remaining() < 3 {
		return "", testOperator{}, false
	}
	lit := tp.peek(1)
	op := testOperators[lit]
	return lit, op, op.isBinary()
}

func (tp *testArgParser) primary() syntax.TestExpr {
	if tp.remaining() == 0 {
		return nil
	}
	if name, op, ok := tp.binaryAhead(); ok {
		return tp.binary(name, op)
	}
	lit := tp.peek(0)
	if lit == "(" && tp.remaining() > 2 {
		open := tp.take()
		paren := &syntax.ParenTest{Lparen: open.Pos(), X: tp.or()}
		if tp.peek(0) == ")" {
			paren.Rparen = tp.take().Pos()
		} else {
			tp.unclosed = true
		}
		return paren
	}
	if op := testOperators[lit]; op.kind == unaryTest && tp.remaining() > 1 {
		opWord := tp.take()
		return &syntax.UnaryTest{OpPos: opWord.Pos(), Op: op.unary, X: tp.take()}
	}
	if name := tp.peek(1); tp.remaining() > 2 && name != testAnd && name != testOr && isDashOperator(name) {
		return tp.binary(name, testOperator{})
	}
	if isDashOperator(lit) && tp.remaining() > 1 {
		opWord := tp.take()
		tp.opaque = append(tp.opaque, opaqueTest{op: opWord, name: lit, x: tp.take()})
		return opWord
	}
	return tp.take()
}

func (tp *testArgParser) binary(name string, op testOperator) syntax.TestExpr {
	x, opWord, y := tp.take(), tp.take(), tp.take()
	if binary, ok := op.treeBinary(); ok {
		return &syntax.BinaryTest{OpPos: opWord.Pos(), Op: binary, X: x, Y: y}
	}
	tp.opaque = append(tp.opaque, opaqueTest{op: opWord, name: name, x: x, y: y})
	return x
}

func walkTest(expr syntax.TestExpr, visit func(e syntax.TestExpr, negation *syntax.UnaryTest)) {
	walkTestUnder(expr, nil, visit)
}

func walkTestUnder(expr syntax.TestExpr, negation *syntax.UnaryTest, visit func(syntax.TestExpr, *syntax.UnaryTest)) {
	if expr == nil {
		return
	}
	visit(expr, negation)
	switch e := expr.(type) {
	case *syntax.UnaryTest:
		if e.Op == syntax.TsNot {
			walkTestUnder(e.X, e, visit)
		}
	case *syntax.BinaryTest:
		if e.Op == syntax.AndTest || e.Op == syntax.OrTest {
			walkTestUnder(e.X, nil, visit)
			walkTestUnder(e.Y, nil, visit)
		}
	case *syntax.ParenTest:
		walkTestUnder(e.X, nil, visit)
	}
}

type operandShape uint8

const (
	shapeNullary operandShape = iota
	shapeUnary
	shapeLeft
	shapeRight
)

type testOperand struct {
	word     *syntax.Word
	shape    operandShape
	op       string
	opSpan   parser.Span
	negation parser.Span
	negated  bool
	double   bool
	start    int
}

func (o testOperand) binary() bool { return o.shape == shapeLeft || o.shape == shapeRight }

func (p *pass) testOperands(t testCommand, visit func(testOperand)) {
	walkTest(t.expr, func(e syntax.TestExpr, negation *syntax.UnaryTest) {
		o := testOperand{double: t.style == doubleBracket}
		if negation != nil {
			_, o.negation = p.testOperator(t, negation.OpPos, 1)
			o.negated = true
		}
		switch e := e.(type) {
		case *syntax.Word:
			if i := slices.IndexFunc(t.opaque, func(op opaqueTest) bool { return op.anchor() == e }); i >= 0 {
				p.opaqueOperands(t.opaque[i], o, visit)
				return
			}
			o.word, o.start = e, parser.Offset(e.Pos())
			visit(o)
		case *syntax.UnaryTest:
			w, ok := e.X.(*syntax.Word)
			if !ok || e.Op == syntax.TsNot {
				return
			}
			o.op, o.opSpan = p.testOperator(t, e.OpPos, 2)
			o.word, o.shape, o.start = w, shapeUnary, parser.Offset(e.OpPos)
			visit(o)
		case *syntax.BinaryTest:
			if e.Op == syntax.AndTest || e.Op == syntax.OrTest {
				return
			}
			o.op, o.opSpan = p.testOperator(t, e.OpPos, len(e.Op.String()))
			o.start = parser.Offset(e.Pos())
			binaryOperands(e.X, e.Y, o, visit)
		}
	})
}

func (p *pass) opaqueOperands(op opaqueTest, o testOperand, visit func(testOperand)) {
	o.op = op.name
	o.opSpan, _ = p.span(op.op)
	if op.unary() {
		o.word, o.shape, o.start = op.x, shapeUnary, parser.Offset(op.op.Pos())
		visit(o)
		return
	}
	o.start = parser.Offset(op.x.Pos())
	binaryOperands(op.x, op.y, o, visit)
}

func binaryOperands(x, y syntax.TestExpr, o testOperand, visit func(testOperand)) {
	if w, ok := x.(*syntax.Word); ok {
		o.word, o.shape = w, shapeLeft
		visit(o)
	}
	if w, ok := y.(*syntax.Word); ok {
		o.word, o.shape = w, shapeRight
		visit(o)
	}
}

func (p *pass) testOperator(t testCommand, pos syntax.Pos, length int) (string, parser.Span) {
	if t.style == doubleBracket {
		span := posSpan(pos, length)
		return p.spanText(span), span
	}
	i, _ := slices.BinarySearchFunc(t.args, pos.Offset(), func(w *syntax.Word, offset uint) int { return cmp.Compare(w.Pos().Offset(), offset) })
	name, _ := parser.Literal(t.args[i])
	span, _ := p.span(t.args[i])
	return name, span
}
