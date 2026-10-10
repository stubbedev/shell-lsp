package lint

import (
	"strings"
	"testing"

	"mvdan.cc/sh/v3/syntax"
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

func TestParseTestArgsCollectsUnknownOperators(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		src  string
		want string
	}{
		{`[ "$a" -newer "$b" ]`, "-newer binary"},
		{`[ -M "$log" ]`, "-M unary"},
		{`[ ! -M x -a -f y ]`, "-M unary"},
		{`[ -n "$x" -o "$y" -nt z ]`, ""},
		{"[ grep -q root /etc/passwd ]", ""},
	} {
		got, ok := testCommandOf(firstCall(t, tc.src+"\n"))
		if !ok {
			t.Fatalf("%q is not a test command", tc.src)
		}
		var ops []string
		for _, op := range got.unknown {
			kind := "binary"
			if op.unary {
				kind = "unary"
			}
			ops = append(ops, op.name+" "+kind)
		}
		if strings.Join(ops, ",") != tc.want {
			t.Errorf("%q unknown operators = %v, want %q", tc.src, ops, tc.want)
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
