package semantic

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/structure"
)

func describe(text string, tokens []Token) []string {
	out := make([]string, len(tokens))
	for i, t := range tokens {
		var mods []string
		for bit, name := range ModifierNames {
			if t.Modifiers&(1<<bit) != 0 {
				mods = append(mods, name)
			}
		}
		out[i] = fmt.Sprintf("%s:%s%s", text[t.Span.Start:t.Span.End], TypeNames[t.Type], strings.Join(append([]string{""}, mods...), "+"))
	}
	return out
}

func TestTokens(t *testing.T) {
	t.Parallel()

	text := "#!/bin/bash\nreadonly MAX=3\ngreet() {\n\tlocal who=\"$1\"\n\techo \"hi $who\" 'raw' >&2\n}\nif (( MAX > 2 )); then greet -v \"$HOME\" | grep x; fi\ncat <<EOF\nbody $MAX\nEOF\n"
	got := describe(text, Tokens(parser.Parse(text, dialect.Bash)))
	want := []string{
		"#!/bin/bash:comment",
		"readonly:function+defaultLibrary",
		"MAX:variable+declaration+readonly",
		"greet:function+declaration",
		"local:function+defaultLibrary",
		"who:variable+declaration",
		"\":string",
		"1:parameter",
		"\":string",
		"echo:function+defaultLibrary",
		"\":string",
		"hi :string",
		"who:variable",
		"\":string",
		"'raw':string",
		">&:operator",
		"if:keyword",
		"MAX:variable+readonly",
		">:operator",
		"2:number",
		"then:keyword",
		"greet:function",
		"-v:property",
		"\":string",
		"HOME:variable+defaultLibrary",
		"\":string",
		"|:operator",
		"grep:function+external",
		"fi:keyword",
		"cat:function+external",
		"<<:operator",
		"EOF:string",
		"body :string",
		"MAX:variable+readonly",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("tokens:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestTokensAreSingleLineAndOrdered(t *testing.T) {
	t.Parallel()

	text := "echo 'multi\nline\nstring' \"a\nb\"\n"
	tokens := Tokens(parser.Parse(text, dialect.Bash))
	last := -1
	for _, tok := range tokens {
		if strings.Contains(text[tok.Span.Start:tok.Span.End], "\n") {
			t.Errorf("token %q spans lines", text[tok.Span.Start:tok.Span.End])
		}
		if tok.Span.Start < last {
			t.Errorf("tokens overlap or are unordered at %d", tok.Span.Start)
		}
		last = tok.Span.End
	}
	if got := describe(text, tokens); len(got) < 5 {
		t.Errorf("multi-line string tokens = %v", got)
	}
}

func TestSelectionPath(t *testing.T) {
	t.Parallel()

	text := "if true; then\n\techo \"$HOME/x\" | grep y\nfi\n"
	offset := strings.Index(text, "HOME")
	var got []string
	for _, s := range structure.SelectionPath(parser.Parse(text, dialect.Bash), offset) {
		got = append(got, text[s.Start:s.End])
	}
	want := []string{"HOME", "$HOME", "\"$HOME/x\"", "echo \"$HOME/x\"", "echo \"$HOME/x\" | grep y", "if true; then\n\techo \"$HOME/x\" | grep y\nfi", text}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("selection path:\n%q\nwant\n%q", got, want)
	}
}
