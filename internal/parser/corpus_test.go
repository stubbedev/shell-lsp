package parser

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

var update = flag.Bool("update", false, "rewrite golden snapshot files")

func TestCorpusSnapshots(t *testing.T) {
	t.Parallel()

	fixtures, err := filepath.Glob(filepath.Join("testdata", "corpus", "*", "*"))
	if err != nil {
		t.Fatalf("listing corpus: %v", err)
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
				t.Fatalf("reading fixture: %v", err)
			}
			got := snapshot(t, string(src), Parse(string(src), d))
			golden := strings.TrimSuffix(fixture, filepath.Ext(fixture)) + ".golden"
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o600); err != nil {
					t.Fatalf("writing golden: %v", err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("reading golden (run with -update to create): %v", err)
			}
			if got != string(want) {
				t.Errorf("snapshot mismatch for %s (run with -update after review)\n--- got\n%s\n--- want\n%s", fixture, got, want)
			}
		})
	}
}

func snapshot(t *testing.T, text string, tree *Tree) string {
	t.Helper()

	var sb strings.Builder
	at := func(s Span) string {
		return fmt.Sprintf("%s-%s %q", lineCol(text, s.Start), lineCol(text, s.End), text[s.Start:s.End])
	}
	fmt.Fprintf(&sb, "dialect %s\n", tree.Dialect)
	for _, e := range tree.Errors {
		fmt.Fprintf(&sb, "error kind=%d %s: %s\n", e.Kind, at(e.Span), e.Message)
	}
	for i, s := range tree.Scopes {
		fmt.Fprintf(&sb, "scope %d parent=%d %s-%s\n", i, s.Parent, lineCol(text, s.Span.Start), lineCol(text, s.Span.End))
	}
	for _, f := range tree.Functions {
		fmt.Fprintf(&sb, "function %s body=%d within=%d doc=%q\n", at(f.NameSpan), f.Body, f.Within, f.Doc)
	}
	for _, d := range tree.Definitions {
		fmt.Fprintf(&sb, "definition %s kind=%d attrs=%06b local=%t within=%d\n", at(d.NameSpan), d.Kind, d.Attrs, d.Local, d.Within)
	}
	for _, r := range tree.References {
		fmt.Fprintf(&sb, "reference %s within=%d\n", at(r.Span), r.Within)
	}
	for _, c := range tree.Commands {
		fmt.Fprintf(&sb, "command %s args=%d within=%d\n", at(c.NameSpan), len(c.Args), c.Within)
	}
	for _, r := range tree.Redirects {
		fmt.Fprintf(&sb, "redirect %s target=%s\n", at(r.OpSpan), at(r.Target))
	}
	for _, h := range tree.Heredocs {
		fmt.Fprintf(&sb, "heredoc %q quoted=%t striptabs=%t body=%s\n", h.Delimiter, h.Quoted, h.StripTabs, at(h.Body))
	}
	for _, c := range tree.Comments {
		fmt.Fprintf(&sb, "comment %s\n", at(c.Span))
	}
	for _, k := range tree.Keywords {
		if text[k.Span.Start:k.Span.End] != k.Name {
			t.Errorf("keyword %s at %s spells %q", k.Name, lineCol(text, k.Span.Start), text[k.Span.Start:k.Span.End])
		}
		fmt.Fprintf(&sb, "keyword %s\n", at(k.Span))
	}
	return sb.String()
}

func lineCol(text string, offset int) string {
	line := strings.Count(text[:offset], "\n") + 1
	col := offset - strings.LastIndexByte(text[:offset], '\n')
	return fmt.Sprintf("%d:%d", line, col)
}
