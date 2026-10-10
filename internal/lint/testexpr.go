package lint

import (
	"path"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/parser"
)

type bracketStyle uint8

const (
	singleBracket bracketStyle = iota
	doubleBracket
	testBuiltin
)

type testCommand struct {
	style bracketStyle
	name  string
	node  syntax.Command
	args  []*syntax.Word
	expr  syntax.TestExpr

	unterminated bool
}

func (t testCommand) bracketed() bool { return t.style != testBuiltin }

func testCommandOf(cmd syntax.Command) (testCommand, bool) {
	switch cmd := cmd.(type) {
	case *syntax.TestClause:
		return testCommand{style: doubleBracket, name: "[[", node: cmd, expr: cmd.X}, true
	case *syntax.CallExpr:
		if len(cmd.Args) == 0 {
			return testCommand{}, false
		}
		name, ok := parser.Literal(cmd.Args[0])
		if !ok {
			return testCommand{}, false
		}
		args := cmd.Args[1:]
		switch {
		case name == "[":
			closed := len(args) > 0 && args[len(args)-1].Lit() == "]"
			if closed {
				args = args[:len(args)-1]
			}
			return testCommand{style: singleBracket, name: name, node: cmd, args: args, expr: parseTestArgs(args), unterminated: !closed}, true
		case name == "test" || (path.Base(name) == "test" && path.Dir(name) != "."):
			return testCommand{style: testBuiltin, name: name, node: cmd, args: args, expr: parseTestArgs(args)}, true
		}
	}
	return testCommand{}, false
}

func isTestCommand(stmt *syntax.Stmt) bool {
	_, ok := testCommandOf(stmt.Cmd)
	return ok
}

var (
	unaryTestOps  = unaryTestOpTable()
	binaryTestOps = binaryTestOpTable()
)

func unaryTestOpTable() map[string]syntax.UnTestOperator {
	out := map[string]syntax.UnTestOperator{"-a": syntax.TsExists, "-h": syntax.TsSmbLink}
	for op := syntax.TsExists; op <= syntax.TsRefVar; op++ {
		out[op.String()] = op
	}
	return out
}

func binaryTestOpTable() map[string]syntax.BinTestOperator {
	out := map[string]syntax.BinTestOperator{}
	for op := syntax.TsReMatch; op <= syntax.TsGtr; op++ {
		out[op.String()] = op
	}
	for _, op := range []syntax.BinTestOperator{syntax.TsMatchShort, syntax.TsMatch, syntax.TsNoMatch, syntax.TsBefore, syntax.TsAfter} {
		out[op.String()] = op
	}
	return out
}

type testArgParser struct {
	words []*syntax.Word
	pos   int
}

func parseTestArgs(words []*syntax.Word) syntax.TestExpr {
	if len(words) == 0 {
		return nil
	}
	tp := &testArgParser{words: words}
	return tp.or()
}

func (tp *testArgParser) peek(ahead int) (string, bool) {
	i := tp.pos + ahead
	if i >= len(tp.words) {
		return "", false
	}
	return parser.Literal(tp.words[i])
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
		if lit, _ := tp.peek(0); lit != "-o" {
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
		if lit, _ := tp.peek(0); lit != "-a" {
			break
		}
		op := tp.take()
		x = &syntax.BinaryTest{OpPos: op.Pos(), Op: syntax.AndTest, X: x, Y: tp.not()}
	}
	return x
}

func (tp *testArgParser) not() syntax.TestExpr {
	if lit, _ := tp.peek(0); lit == "!" && tp.remaining() > 1 {
		if _, binary := tp.binaryAhead(1); !binary {
			op := tp.take()
			return &syntax.UnaryTest{OpPos: op.Pos(), Op: syntax.TsNot, X: tp.not()}
		}
	}
	return tp.primary()
}

func (tp *testArgParser) binaryAhead(ahead int) (syntax.BinTestOperator, bool) {
	lit, ok := tp.peek(ahead)
	if !ok || tp.remaining() <= ahead+1 {
		return 0, false
	}
	op, binary := binaryTestOps[lit]
	return op, binary
}

func (tp *testArgParser) primary() syntax.TestExpr {
	if tp.remaining() == 0 {
		return nil
	}
	if op, ok := tp.binaryAhead(1); ok {
		x := tp.take()
		opWord := tp.take()
		return &syntax.BinaryTest{OpPos: opWord.Pos(), Op: op, X: x, Y: tp.take()}
	}
	lit, _ := tp.peek(0)
	if lit == "(" && tp.remaining() > 2 {
		open := tp.take()
		x := tp.or()
		paren := &syntax.ParenTest{Lparen: open.Pos(), X: x}
		if closing, _ := tp.peek(0); closing == ")" {
			paren.Rparen = tp.take().Pos()
		}
		return paren
	}
	if op, ok := unaryTestOps[lit]; ok && tp.remaining() > 1 {
		opWord := tp.take()
		return &syntax.UnaryTest{OpPos: opWord.Pos(), Op: op, X: tp.take()}
	}
	return tp.take()
}

func walkTest(expr syntax.TestExpr, visit func(syntax.TestExpr)) {
	if expr == nil {
		return
	}
	visit(expr)
	switch e := expr.(type) {
	case *syntax.UnaryTest:
		if e.Op == syntax.TsNot {
			walkTest(e.X, visit)
		}
	case *syntax.BinaryTest:
		if e.Op == syntax.AndTest || e.Op == syntax.OrTest {
			walkTest(e.X, visit)
			walkTest(e.Y, visit)
		}
	case *syntax.ParenTest:
		walkTest(e.X, visit)
	}
}
