package parser

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

func TestGlobQualifiers(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		word       string
		bare, want int
	}{
		{word: "*.txt(N)", bare: 1, want: 0},
		{word: "**/*(.DOL[1,3])", bare: 1, want: 0},
		{word: "$dump(#qN.mh+24)", bare: 1, want: 1},
		{word: "*(#q.)(N)", bare: 2, want: 0},
		{word: "*(#q.)(#qN)", bare: 2, want: 2},
		{word: "*(e:'(( 1 ))':)", bare: 1, want: 0},
		{word: "*.(zsh|sh)", bare: 0, want: 0},
		{word: "a(b|c)(.)", bare: 1, want: 0},
		{word: "((a|b)|c)*", bare: 0, want: 0},
		{word: "x(#i)y", bare: 0, want: 0},
		{word: "foo(^x~y)", bare: 0, want: 0},
		{word: "plain", bare: 0, want: 0},
	} {
		tree := Parse("print "+tc.word+"\n", dialect.Zsh)
		call, ok := tree.File.Stmts[0].Cmd.(*syntax.CallExpr)
		if !ok || len(call.Args) != 2 {
			t.Fatalf("%s: not one argument", tc.word)
		}
		w := call.Args[1]
		if got := len(w.Parts) - GlobQualifiers(w, true); got != tc.bare {
			t.Errorf("%s: %d qualifier parts with bare lists, want %d", tc.word, got, tc.bare)
		}
		if got := len(w.Parts) - GlobQualifiers(w, false); got != tc.want {
			t.Errorf("%s: %d explicit qualifier parts, want %d", tc.word, got, tc.want)
		}
	}
}
