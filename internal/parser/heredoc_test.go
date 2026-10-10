package parser

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

func TestNearMissEndToken(t *testing.T) {
	t.Parallel()

	for line, want := range map[string]EndTokenMistake{
		"EOF ":      TrailingWhitespace,
		"EOF)":      ClosingParenthesis,
		"EOF  )":    ClosingParenthesis,
		"EOF # end": TrailingComment,
		"EOF;":      TrailingOperator,
		"EOF | tr":  TrailingOperator,
		"EOF now":   TrailingText,
		"EOFX":      0,
		"EOF":       0,
		"xEOF ":     0,
	} {
		got, _, _ := NearMissEndToken(line, "EOF")
		if got != want {
			t.Errorf("NearMissEndToken(%q) = %v, want %v", line, got, want)
		}
	}
}

func TestUnclosedHeredocNamesTheNearMiss(t *testing.T) {
	t.Parallel()

	tree := Parse("cat <<EOF\nhello\nEOF \n", dialect.Bash)
	want := "here-document `EOF` is never closed: line 3 starts with the end token but is followed by whitespace"
	if got := messages(tree); !slices.Contains(got, want) {
		t.Errorf("messages = %q, want %q", got, want)
	}
}
