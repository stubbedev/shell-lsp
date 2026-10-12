package lint

import (
	"cmp"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

func TestParseTestArgs(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		src  string
		want string
	}{
		{"[ -a foo ]", "(-e foo)"},
		{`[ "$x" -a "$y" ]`, "(word && word)"},
		{`[ ! -z "$x" ]`, "(! (-z word))"},
		{"[ a = b -o c != d ]", "((a = b) || (c != d))"},
		{`[ \( -n x \) ]`, "[(-n x)]"},
		{"[ -n ]", "-n"},
		{"[ ! ]", "!"},
		{"test x = y", "(x = y)"},
	} {
		got, ok := testCommandOf(firstCall(t, tc.src+"\n"))
		if !ok {
			t.Fatalf("%q is not a test command", tc.src)
		}
		if shape := testShape(got.expr); shape != tc.want {
			t.Errorf("%q parsed as %s, want %s", tc.src, shape, tc.want)
		}
	}
}

func TestParseTestArgsCollectsOpaqueOperators(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		src      string
		want     string
		complete bool
	}{
		{`[ "$a" -newer "$b" ]`, "-newer binary", true},
		{`[ -M "$log" ]`, "-M unary", true},
		{`[ ! -M x -a -f y ]`, "-M unary", true},
		{`[ -n "$x" -o "$y" -nt z ]`, "", true},
		{`[ "$a" \<= "$b" ]`, "<= binary", true},
		{"[ \u2013f x ]", "\u2013f unary", true},
		{"[ grep -q root /etc/passwd ]", "-q binary", false},
	} {
		got, ok := testCommandOf(firstCall(t, tc.src+"\n"))
		if !ok {
			t.Fatalf("%q is not a test command", tc.src)
		}
		var ops []string
		for _, op := range got.opaque {
			kind := "binary"
			if op.unary() {
				kind = "unary"
			}
			ops = append(ops, op.name+" "+kind)
		}
		if strings.Join(ops, ",") != tc.want || got.complete != tc.complete {
			t.Errorf("%q opaque operators = %v, complete %t, want %q, complete %t", tc.src, ops, got.complete, tc.want, tc.complete)
		}
	}
}

func TestTestOperandsAgreeAcrossBrackets(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		src  string
		want string
	}{
		{"[ -a x ]", "x -a(-a) unary"},
		{"[[ -a x ]]", "x -a(-a) unary [["},
		{`[ "-h" x ]`, `x -h("-h") unary`},
		{"[ ! -o x ]", "x -o(-o) unary !"},
		{"[[ ! -o x ]]", "x -o(-o) unary ! [["},
		{"[ a = b -a c ]", "a =(=) left|b =(=) right|c () nullary"},
		{"[[ a = b && c ]]", "a =(=) left [[|b =(=) right [[|c () nullary [["},
		{`[ \( x -foo y \) ]`, "x -foo(-foo) left|y -foo(-foo) right"},
		{"[ ! a '>=' b ]", "a >=('>=') left !|b >=('>=') right !"},
	} {
		tree := parser.Parse(tc.src+"\n", dialect.Bash)
		p := newPass(tree, nil, Options{})
		test, ok := testCommandOf(tree.File.Stmts[0].Cmd)
		if !ok {
			t.Fatalf("%q is not a test command", tc.src)
		}
		var got []string
		p.testOperands(test, func(o testOperand) {
			entry := p.nodeText(o.word) + " " + o.op + "(" + p.spanText(o.opSpan) + ") " + [...]string{"nullary", "unary", "left", "right"}[o.shape]
			if o.negated {
				entry += " " + p.spanText(o.negation)
			}
			if o.double {
				entry += " [["
			}
			got = append(got, entry)
		})
		if strings.Join(got, "|") != tc.want {
			t.Errorf("%q operands = %q, want %q", tc.src, strings.Join(got, "|"), tc.want)
		}
	}
}

func TestTestOperatorsMatchSyntaxTree(t *testing.T) {
	t.Parallel()

	aliases := map[string]string{"-a": "-e", "-h": "-L"}
	for name, op := range testOperators {
		want := cmp.Or(aliases[name], name)
		switch op.kind {
		case unaryTest:
			if got := op.unary.String(); got != want {
				t.Errorf("%s maps to the unary operator %s", name, got)
			}
		case inclusiveOrderTest:
			if _, ok := op.treeBinary(); ok {
				t.Errorf("%s has no syntax tree operator but maps to %s", name, op.binary)
			}
		default:
			if got, ok := op.treeBinary(); !ok || got.String() != want {
				t.Errorf("%s maps to the binary operator %s", name, got)
			}
		}
	}
}

func testShape(e syntax.TestExpr) string {
	switch e := e.(type) {
	case *syntax.UnaryTest:
		return "(" + e.Op.String() + " " + testShape(e.X) + ")"
	case *syntax.BinaryTest:
		return "(" + testShape(e.X) + " " + e.Op.String() + " " + testShape(e.Y) + ")"
	case *syntax.ParenTest:
		return "[" + testShape(e.X) + "]"
	case *syntax.Word:
		if lit := e.Lit(); lit != "" {
			return lit
		}
		return "word"
	}
	return "?"
}
