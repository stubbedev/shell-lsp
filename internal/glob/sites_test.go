package glob

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/shelloptions"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

func TestRegexErrorsPointInsideTheQuotes(t *testing.T) {
	t.Parallel()

	src := "[[ $v =~ 'ab(c' ]]\n"
	tree := parser.Parse(src, dialect.Zsh)
	sites := For(tree, shelloptions.Scan(tree)).All()
	if len(sites) != 1 || len(sites[0].Pattern.Errors) != 1 {
		t.Fatalf("sites = %+v, want one site with one error", sites)
	}
	if at := sites[0].Pattern.Errors[0].Span; src[at.Start:at.End] != "(" {
		t.Errorf("error covers %q, want the opening parenthesis", src[at.Start:at.End])
	}
}

func TestResolvedValuesKeepToTheSpanOfTheirExpansion(t *testing.T) {
	t.Parallel()

	src := "[[ $v =~ $re ]]\n"
	tree := parser.Parse(src, dialect.Zsh)
	sites := For(tree, shelloptions.Scan(tree))
	all := sites.All()
	if len(all) != 1 {
		t.Fatalf("sites = %+v, want one", all)
	}
	_, pattern, ok := sites.Pattern(all[0].Word, RoleRegex, func(syntax.WordPart) (string, bool) { return "^([a-z]+)x", true })
	if !ok {
		t.Fatal("the word with a resolved value is not a pattern")
	}
	for _, piece := range pattern.Pieces(src) {
		if piece.Text != "$re" {
			t.Errorf("piece %q at %+v, want the $re expansion", piece.Text, piece.Span)
		}
	}
}

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
