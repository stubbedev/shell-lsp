package lint

import (
	"testing"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func flowOf(t *testing.T, src string) (*parser.Tree, *flowResult) {
	t.Helper()
	tree := parser.Parse(src, dialect.Bash)
	return tree, analyzeFlow(tree, nil, false)
}

func namedCalls(tree *parser.Tree, name string) []*syntax.CallExpr {
	var out []*syntax.CallExpr
	syntax.Walk(tree.File, func(n syntax.Node) bool {
		if call, ok := n.(*syntax.CallExpr); ok && len(call.Args) > 0 && call.Args[0].Lit() == name {
			out = append(out, call)
		}
		return true
	})
	return out
}

func TestFlowForwardCalls(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		src  string
		want []bool
	}{
		{"f\nf() { :; }\n", []bool{true}},
		{"false || f\nf() { :; }\n", []bool{true}},
		{"f || f() { :; }\n", []bool{false}},
		{"f\nexit 1\nf() { :; }\n", []bool{false}},
		{"f() { :; }\nf\n", []bool{false}},
		{"x=$(f)\nf() { :; }\n", []bool{true}},
		{"f\nif true; then f() { :; }; fi\n", []bool{false}},
	} {
		tree, flow := flowOf(t, tc.src)
		calls := namedCalls(tree, "f")
		if len(calls) != len(tc.want) {
			t.Fatalf("%q: %d calls, want %d", tc.src, len(calls), len(tc.want))
		}
		for i, call := range calls {
			if got := flow.forwardCall(call); got != tc.want[i] {
				t.Errorf("%q call %d: forward = %t, want %t", tc.src, i, got, tc.want[i])
			}
		}
	}
}

func TestFlowDeadFunctions(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		src  string
		dead bool
	}{
		{"f() { :; }\nexit 0\n", true},
		{"f() { :; }\n", false},
		{"f() { :; }\nf\nexit 0\n", false},
		{"g() { f; }\nf() { :; }\ng\nexit 0\n", false},
		{"f() { :; }\nreturn\n", false},
		{"if x; then f() { :; }; else f() { echo; }; fi\n", false},
		{"g() { f() { :; }; }\n", true},
	} {
		tree, flow := flowOf(t, tc.src)
		fn, ok := tree.FunctionNamed("f")
		if !ok {
			t.Fatalf("%q: no function f", tc.src)
		}
		if got := flow.deadFunctions[fn.Decl]; got != tc.dead {
			t.Errorf("%q: dead = %t, want %t", tc.src, got, tc.dead)
		}
	}
}

func TestFlowStatusReads(t *testing.T) {
	t.Parallel()

	tree, flow := flowOf(t, "a\nif b; then c; else d; fi\necho $?\n")
	var read *syntax.ParamExp
	syntax.Walk(tree.File, func(n syntax.Node) bool {
		if pe, ok := n.(*syntax.ParamExp); ok && pe.Param.Value == "?" {
			read = pe
		}
		return true
	})
	var got []string
	for _, setter := range flow.statusReads[read] {
		got = append(got, setter.Cmd.(*syntax.CallExpr).Args[0].Lit())
	}
	if len(got) != 2 || got[0] != "c" || got[1] != "d" {
		t.Errorf("status setters = %v, want [c d]", got)
	}
}

func TestFlowNumericAppends(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		src     string
		numeric bool
		integer bool
	}{
		{"x+=1\n", true, false},
		{"n=4\nx+=n\n", true, false},
		{"n=foo\nx+=$n\n", false, false},
		{"n=$((2*2))\nx+=$n\n", true, false},
		{"declare -i x=0\nx+=1\n", true, true},
		{"x+='1'\n", false, false},
	} {
		tree, flow := flowOf(t, tc.src)
		var fact appendFact
		syntax.Walk(tree.File, func(n syntax.Node) bool {
			if a, ok := n.(*syntax.Assign); ok && a.Append {
				fact = flow.appends[a]
			}
			return true
		})
		if fact.numeric != tc.numeric || fact.integer != tc.integer {
			t.Errorf("%q: append = %+v, want numeric %t integer %t", tc.src, fact, tc.numeric, tc.integer)
		}
	}
}

func TestFlowReachability(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		src     string
		reached bool
	}{
		{"for i in 1; do break; probe; done\n", true},
		{"while true; do :; done\nprobe\n", true},
		{"exit 1\nprobe\n", false},
		{"die() { exit 1; }\ndie\nprobe\n", false},
		{"x=$(exit 1; probe)\n", false},
		{"(exit 1)\nprobe\n", true},
	} {
		tree, flow := flowOf(t, tc.src)
		var probe *syntax.Stmt
		syntax.Walk(tree.File, func(n syntax.Node) bool {
			if stmt, ok := n.(*syntax.Stmt); ok {
				if call, ok := stmt.Cmd.(*syntax.CallExpr); ok && len(call.Args) > 0 && call.Args[0].Lit() == "probe" {
					probe = stmt
				}
			}
			return true
		})
		if got := flow.reached[probe]; got != tc.reached {
			t.Errorf("%q: reached = %t, want %t", tc.src, got, tc.reached)
		}
	}
}

func TestStatusSetUnion(t *testing.T) {
	t.Parallel()

	tree := parser.Parse("a\nb\nc\n", dialect.Bash)
	a, b, c := tree.File.Stmts[0], tree.File.Stmts[1], tree.File.Stmts[2]
	got := statusSet{a, c}.union(statusSet{b, c})
	if len(got) != 3 || got[0] != a || got[1] != b || got[2] != c {
		t.Errorf("union = %v, want [a b c] in source order", got)
	}
}
