package parser

import (
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
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

func TestUnclosedHeredocRunsToTheEndOfTheText(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		src, command, body string
		bodyLiteral        string
	}{
		{src: "echo hi\ncat <<END | grep x\nbody [x]\n  END\n", command: "cat <<END | grep x", body: "body [x]\n  END\n", bodyLiteral: "body [x]\n  END\n"},
		{src: "cat <<'END'\nbody [x]", command: "cat <<'END'", body: "body [x]", bodyLiteral: "body [x]"},
		{src: "cat <<END\nvalue $x", command: "cat <<END", body: "value $x"},
		{src: "cat <<END\n", command: "cat <<END", body: ""},
	} {
		tree := Parse(tc.src, dialect.Bash)
		if len(tree.Errors) != 1 {
			t.Errorf("%q: errors = %q, want only the unclosed here-document", tc.src, messages(tree))
		}
		if len(tree.Heredocs) != 1 {
			t.Fatalf("%q: heredocs = %+v, want one", tc.src, tree.Heredocs)
		}
		h := tree.Heredocs[0]
		if tc.src[h.Body.Start:h.Body.End] != tc.body {
			t.Errorf("%q: body = %q, want %q", tc.src, tc.src[h.Body.Start:h.Body.End], tc.body)
		}
		last := tree.File.Stmts[len(tree.File.Stmts)-1]
		span, ok := tree.NodeSpan(last)
		if !ok || span.Start > h.Span.Start || !strings.HasPrefix(tc.src[span.Start:], tc.command) {
			t.Errorf("%q: last statement %+v, want the %q line, not the body", tc.src, span, tc.command)
		}
		if tc.bodyLiteral == "" {
			continue
		}
		var hdoc *syntax.Word
		Walk(last, func(n syntax.Node) bool {
			if r, ok := n.(*syntax.Redirect); ok && r.Hdoc != nil {
				hdoc = r.Hdoc
			}
			return true
		})
		if got, _ := Literal(hdoc); got != tc.bodyLiteral {
			t.Errorf("%q: here-document literal = %q, want %q", tc.src, got, tc.bodyLiteral)
		}
	}
}

func TestUnclosableHeredocStopsRecovering(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		src string
		d   dialect.Dialect
	}{
		{"cat <<EOF\nrun ` now\nEOF\n", dialect.Bash},
		{"cat <<EOF $(run", dialect.Bash},
		{"cat <<EOF `run", dialect.Bash},
		{"<<EOF (run", dialect.Zsh},
		{"<<EOF (run", dialect.Ksh},
		{"<<$\"A", dialect.Zsh},
		{"<<0(\n", dialect.Bash},
		{"<<0(\n#\n(", dialect.Bash},
		{"<<0\\", dialect.Bash},
		{"<<&\\\x00", dialect.Bash},
		{"<<#\\", dialect.Bash},
	} {
		tree := Parse(tc.src, tc.d)
		if len(tree.Errors) == 0 {
			t.Errorf("%s %q: want the unclosed here-document reported", tc.d, tc.src)
		}
		Walk(tree.File, func(n syntax.Node) bool {
			if n == nil {
				return true
			}
			if span, ok := tree.NodeSpan(n); ok && span.End > len(tc.src) {
				t.Errorf("%s %q: %T ends at %d, past the text", tc.d, tc.src, n, span.End)
			}
			return true
		})
		assertSpansInText(t, tc.d, tc.src, tree)
	}
}

func TestUnclosedHeredocDoesNotIndexTheRecoveryDelimiter(t *testing.T) {
	t.Parallel()

	for _, src := range []string{"<<0(\n#\n(", "<<0(\n#\n(\n"} {
		tree := Parse(src, dialect.Bash)
		if len(tree.Commands) != 0 {
			t.Errorf("%q: commands = %+v, want none", src, tree.Commands)
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
