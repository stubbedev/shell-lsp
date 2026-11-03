package nix

import (
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
