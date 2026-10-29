package glob

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/shelloptions"
)

func TestWordsWithOverlappingPartsAreNotPatterns(t *testing.T) {
	t.Parallel()

	for _, src := range []string{"$A[0[0[0[0 .#000", "$A[0[0[0[0 .#000\n"} {
		tree := parser.Parse(src, dialect.Zsh)
		if sites := For(tree, shelloptions.Scan(tree)).All(); len(sites) != 0 {
			t.Errorf("%q: sites = %+v, want none", src, sites)
		}
	}
}

func TestOrderedWordsStayPatterns(t *testing.T) {
	t.Parallel()

	src := "print -l *.#000\n"
	tree := parser.Parse(src, dialect.Zsh)
	sites := For(tree, shelloptions.Scan(tree)).All()
	if len(sites) != 1 || src[sites[0].Span.Start:sites[0].Span.End] != "*.#000" {
		t.Fatalf("sites = %+v, want the *.#000 word", sites)
	}
}
