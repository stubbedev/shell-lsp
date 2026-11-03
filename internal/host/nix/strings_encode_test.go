package nix

import (
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func TestAStandInBodyEditEscapesForItsString(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src, old, text, want string
	}{
		{"an antiquotation in an indented string", "''\n  a\n''", "a", "${b}", "''\n  ''${b}\n''"},
		{"two quotes in an indented string", "''\n  a\n''", "a", "''", "''\n  '''\n''"},
		{"a new line takes the indentation", "''\n  a\n''", "a", "b\nc", "''\n  b\n  c\n''"},
		{"an antiquotation in a quoted string", "\"a\"", "a", "${b}", "\"\\${b}\""},
		{"a quote in a quoted string", "\"a\"", "a", "\"", "\"\\\"\""},
		{"a backslash in a quoted string", "\"a\"", "a", "\\", "\"\\\\\""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tree := syntax.Parse(tt.src)
			f := &file{src: tt.src, tree: tree, table: scope.Resolve(tree)}
			str, isString := tree.Root.(*syntax.String)
			if !isString {
				t.Fatalf("%q is no string", tt.src)
			}
			via := f.standInBody(str)
			at := len(via.Text()) - len(tt.old)
			if via.Text()[len(via.Text())-1] == '\n' {
				at--
			}
			edit, ok := via.Edit(textedit.Edit{Start: at, End: at + len(tt.old), NewText: tt.text})
			if !ok {
				t.Fatalf("Edit was refused in %q", via.Text())
			}
			if got := textedit.Apply(tt.src, []textedit.Edit{edit}); got != tt.want {
				t.Errorf("host = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEditsKeepTheIndentOfTheWholeString(t *testing.T) {
	t.Parallel()

	removal := func(text, line string) textedit.Edit {
		start := strings.Index(text, line)
		return textedit.Edit{Start: start, End: start + len(line)}
	}
	for _, tt := range []struct {
		name, body string
		lines      []string
		mapped     bool
	}{
		{"one of two shallowest lines goes", "  a\n  b\n    c\n", []string{"a\n"}, true},
		{"both shallowest lines go", "  a\n  b\n    c\n", []string{"a\n", "b\n"}, false},
		{"a deeper line stays beside a shallow one", "  a\n  b\n    c\n", []string{"a\n", "  c\n"}, true},
		{"a hole with a deep line in it does not hold the indent up", "  a\n    ${\"\n  x\"}\n", []string{"a\n"}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := "''\n" + tt.body + "''"
			tree := syntax.Parse(src)
			f := &file{src: src, tree: tree, table: scope.Resolve(tree)}
			m := f.standInBody(tree.Root.(*syntax.String))
			edits := make([]textedit.Edit, len(tt.lines))
			for i, line := range tt.lines {
				edits[i] = removal(m.Text(), line)
			}
			if _, mapped := m.Edits(edits); mapped != tt.mapped {
				t.Errorf("Edits(%+v) on %q mapped = %v, want %v", edits, m.Text(), mapped, tt.mapped)
			}
		})
	}
}
