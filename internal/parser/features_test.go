package parser_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/actions"
	"github.com/stubbedev/shell-lsp/internal/completion"
	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/format"
	"github.com/stubbedev/shell-lsp/internal/hover"
	"github.com/stubbedev/shell-lsp/internal/manpage"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/semantic"
	"github.com/stubbedev/shell-lsp/internal/structure"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

type offlineCommands struct{}

func (offlineCommands) Describe(context.Context, string) (string, error) { return "external", nil }

type offlineEnv struct{}

func (offlineEnv) Executables() []string                         { return []string{"ls", "grep"} }
func (offlineEnv) ReadDir(string) ([]completion.DirEntry, error) { return nil, os.ErrNotExist }
func (offlineEnv) Home() string                                  { return "/home/u" }
func (offlineEnv) WorkDir() (string, error)                      { return "/work", nil }
func (offlineEnv) EnvironmentNames() []string                    { return []string{"HOME"} }
func (offlineEnv) Options(context.Context, string, []string) ([]manpage.Option, error) {
	return nil, manpage.ErrNoPage
}

func (offlineEnv) Subcommands(context.Context, string) (manpage.Listing, error) {
	return manpage.Listing{}, nil
}

func TestFeaturesOverCorpus(t *testing.T) {
	t.Parallel()

	fixtures, err := filepath.Glob(filepath.Join("testdata", "corpus", "*", "*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		if filepath.Ext(fixture) == ".golden" {
			continue
		}
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()

			d, ok := dialect.FromName(filepath.Base(filepath.Dir(fixture)))
			if !ok {
				t.Fatalf("unknown dialect directory for %s", fixture)
			}
			src, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatal(err)
			}
			text := string(src)
			tree := parser.Parse(text, d)
			inBounds := func(what string, s parser.Span) {
				t.Helper()
				if s.Start < 0 || s.End > len(text) || s.Start > s.End {
					t.Errorf("%s span %+v out of bounds (%d bytes)", what, s, len(text))
				}
			}
			for _, tok := range semantic.Tokens(tree, nil) {
				inBounds("token", tok.Span)
			}
			var walk func([]structure.Symbol)
			walk = func(symbols []structure.Symbol) {
				for _, s := range symbols {
					inBounds("symbol", s.Span)
					walk(s.Children)
				}
			}
			walk(structure.Symbols(tree))
			lines := strings.Count(text, "\n")
			for _, f := range structure.Folds(tree, func(o int) int { return strings.Count(text[:o], "\n") }) {
				if f.StartLine < 0 || f.EndLine > lines || f.StartLine >= f.EndLine {
					t.Errorf("fold %+v invalid for %d lines", f, lines)
				}
			}
			for offset := 0; offset <= len(text); offset += 7 {
				if r, ok, err := hover.Hover(t.Context(), hover.Input{Tree: tree, Offset: offset, Commands: offlineCommands{}}); err != nil {
					t.Errorf("hover at %d: %v", offset, err)
				} else if ok {
					inBounds("hover", r.Span)
				}
				result, err := completion.Complete(t.Context(), completion.Input{Tree: tree, Offset: offset, Snippets: true, Env: offlineEnv{}})
				if err != nil {
					t.Errorf("completion at %d: %v", offset, err)
				}
				for _, it := range result.Items {
					inBounds("completion", it.Replace)
				}
				for _, s := range structure.SelectionPath(tree, offset) {
					inBounds("selection", s)
				}
				for _, kind := range []actions.RefactorKind{actions.ExtractVariable, actions.ExtractFunction, actions.ToggleQuotes, actions.DeclareLocal} {
					edits, _, ok := actions.Compute(tree, actions.Refactor{Kind: kind, Start: offset, End: min(offset+5, len(text))})
					if !ok {
						continue
					}
					for _, e := range edits {
						inBounds("refactor edit", parser.Span{Start: e.Start, End: e.End})
					}
					if refactored := textedit.Apply(text, edits); len(parser.Parse(refactored, d).Errors) > len(tree.Errors) {
						t.Errorf("%s at %d introduced syntax errors:\n%s", kind, offset, refactored)
					}
				}
			}
			if len(tree.Errors) == 0 {
				formatted, err := format.Format(tree, format.Options{})
				if err != nil {
					t.Errorf("formatting a valid document: %v", err)
				} else if got := textedit.Apply(text, format.Edits(text, formatted)); got != formatted {
					t.Error("format edits do not reproduce the formatted text")
				}
			}
		})
	}
}
