package actions

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/format"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

func TestRefactoringsKeepCorpusParsing(t *testing.T) {
	t.Parallel()

	var fixtures []string
	for _, dir := range []string{filepath.Join("..", "parser", "testdata", "corpus"), filepath.Join("testdata", "corpus")} {
		found, err := filepath.Glob(filepath.Join(dir, "*", "*"))
		if err != nil {
			t.Fatalf("listing the corpus in %s: %v", dir, err)
		}
		fixtures = append(fixtures, found...)
	}
	for _, fixture := range fixtures {
		if filepath.Ext(fixture) == ".golden" {
			continue
		}
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()

			d, ok := dialect.FromName(filepath.Base(filepath.Dir(fixture)))
			if !ok {
				t.Fatalf("corpus directory %s is not a dialect name", filepath.Dir(fixture))
			}
			src, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatalf("reading %s: %v", fixture, err)
			}
			text := string(src)
			tree := parser.Parse(text, d)
			if len(tree.Errors) > 0 {
				t.Skipf("%s has syntax errors by design", fixture)
			}
			for _, style := range []format.Options{{}, {Indent: 4, SwitchCaseIndent: true}} {
				for _, sel := range corpusSelections(tree) {
					for _, a := range Actions(Input{Tree: tree, Format: style, Selection: sel}) {
						if a.Refactor == nil {
							continue
						}
						out := textedit.Apply(text, a.Edits)
						if errs := parser.Parse(out, d).Errors; len(errs) > 0 {
							t.Errorf("%s at %d-%d with %+v introduced %q:\n%s", a.Title, sel.Start, sel.End, style, errs[0].Message, out)
						}
					}
				}
			}
		})
	}
}

func corpusSelections(tree *parser.Tree) []parser.Span {
	seen := map[parser.Span]bool{}
	var out []parser.Span
	add := func(s parser.Span) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	parser.Walk(tree.File, func(node syntax.Node) bool {
		if node == nil {
			return true
		}
		if span, ok := tree.NodeSpan(node); ok {
			add(parser.Span{Start: span.Start, End: span.Start})
			add(parser.Span{Start: span.Start + 1, End: span.Start + 1})
			add(span)
		}
		return true
	})
	slices.SortFunc(out, func(a, b parser.Span) int {
		if a.Start != b.Start {
			return a.Start - b.Start
		}
		return a.End - b.End
	})
	return out
}
