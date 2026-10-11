package lint

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func TestTrailingCommaFixRemovesEachComma(t *testing.T) {
	t.Parallel()

	text := "for cmd in ls, 'cp', mv; do command -v \"$cmd\"; done\n"
	var edits []textedit.Edit
	for _, f := range Check(parser.Parse(text, dialect.Bash), nil, Options{}) {
		if f.Code == 2258 {
			edits = append(edits, fixEdits(f)...)
		}
	}
	want := "for cmd in ls 'cp' mv; do command -v \"$cmd\"; done\n"
	if got := textedit.Apply(text, edits); got != want {
		t.Fatalf("applying the SC2258 fixes = %q, want %q", got, want)
	}
}
