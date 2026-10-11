package server

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func TestDocumentDialectSaysWhatDecidedIt(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {}}`)
	for _, tc := range []struct {
		uri, text string
		want      documentDialect
	}{
		{"file:///w/a.sh", "#!/usr/bin/env bash\necho\n", documentDialect{Dialect: "bash", Source: "shebang", Evidence: "#!/usr/bin/env bash", Description: "from the shebang #!/usr/bin/env bash"}},
		{"file:///w/b.sh", "echo\n", documentDialect{Dialect: "sh", Source: "extension", Evidence: ".sh", Guessed: true, Description: "guessed from the .sh extension"}},
		{"file:///w/run", "echo\n", documentDialect{Dialect: "bash", Source: "default", Guessed: true, Description: "guessed: nothing names the shell, so bash is assumed"}},
		{"file:///w/c.sh", "#!/bin/sh\n# shellcheck shell=dash\necho\n", documentDialect{Dialect: "dash", Source: "directive", Evidence: "dash", Description: "from the directive # shellcheck shell=dash", Directive: true}},
	} {
		c.notify("textDocument/didOpen", openParams(tc.uri, tc.text))
		got := decodeResult[documentDialect](t, c.call(documentDialectMethod, `{"textDocument": {"uri": "`+tc.uri+`"}}`))
		tc.want.Dialects = []string{"bash", "sh", "dash", "zsh", "ksh", "busybox"}
		if !equalDialect(got, tc.want) {
			t.Errorf("%s: %+v, want %+v", tc.uri, got, tc.want)
		}
	}
	if missing := c.call(documentDialectMethod, `{"textDocument": {"uri": "file:///w/closed.sh"}}`); missing.Error == nil {
		t.Error("a closed document got a dialect")
	}
}

func equalDialect(a, b documentDialect) bool {
	if len(a.Dialects) != len(b.Dialects) {
		return false
	}
	for i := range a.Dialects {
		if a.Dialects[i] != b.Dialects[i] {
			return false
		}
	}
	a.Dialects, b.Dialects = nil, nil
	return a.Dialect == b.Dialect && a.Source == b.Source && a.Evidence == b.Evidence && a.Guessed == b.Guessed && a.Description == b.Description && a.Directive == b.Directive
}

func TestSetDialectReturnsTheDirectiveEdit(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"workspace": {"workspaceEdit": {"documentChanges": true}}}}`)
	text := "#!/bin/sh\necho\n"
	c.notify("textDocument/didOpen", openParams("file:///w/a.sh", text))
	edit := decodeResult[lsp.WorkspaceEdit](t, c.call(setDialectMethod, `{"textDocument": {"uri": "file:///w/a.sh"}, "dialect": "dash"}`))
	if len(edit.DocumentChanges) != 1 || len(edit.DocumentChanges[0].Edits) != 1 {
		t.Fatalf("edit = %+v", edit)
	}
	change := edit.DocumentChanges[0].Edits[0]
	if change.Range.Start != (lsp.Position{Line: 1}) || change.NewText != "# shellcheck shell=dash\n" || edit.DocumentChanges[0].TextDocument.Version == nil {
		t.Errorf("edit = %+v", change)
	}
	if got := textedit.Apply(text, []textedit.Edit{{Start: 10, End: 10, NewText: change.NewText}}); got != "#!/bin/sh\n# shellcheck shell=dash\necho\n" {
		t.Errorf("applied = %q", got)
	}
	if none := c.call(setDialectMethod, `{"textDocument": {"uri": "file:///w/a.sh"}, "dialect": null}`); none.Error != nil || string(none.Result) != "null" {
		t.Errorf("removing a missing directive = %+v", none)
	}
	if bad := c.call(setDialectMethod, `{"textDocument": {"uri": "file:///w/a.sh"}, "dialect": "fish"}`); bad.Error == nil {
		t.Error("an unknown dialect was accepted")
	}
}
