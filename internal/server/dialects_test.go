package server

import (
	"reflect"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

type dialectAnswer struct {
	Selectable  bool     `json:"selectable"`
	Dialect     string   `json:"dialect"`
	Source      string   `json:"source"`
	Evidence    string   `json:"evidence"`
	Guessed     bool     `json:"guessed"`
	Description string   `json:"description"`
	Directive   *bool    `json:"directive"`
	Dialects    []string `json:"dialects"`
}

func askDialect(t *testing.T, c *testClient, uri string) dialectAnswer {
	t.Helper()

	return decodeResult[dialectAnswer](t, c.call(documentDialectMethod, `{"textDocument": {"uri": "`+uri+`"}}`))
}

func selectable(directive bool, answer dialectAnswer) dialectAnswer {
	answer.Selectable = true
	answer.Directive = &directive
	answer.Dialects = []string{"bash", "sh", "dash", "zsh", "ksh", "busybox"}
	return answer
}

func TestDocumentDialectSaysWhatDecidedIt(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {}}`)
	for _, tc := range []struct {
		uri, text string
		want      dialectAnswer
	}{
		{"file:///w/a.sh", "#!/usr/bin/env bash\necho\n", selectable(false, dialectAnswer{Dialect: "bash", Source: "shebang", Evidence: "#!/usr/bin/env bash", Description: "from the shebang #!/usr/bin/env bash"})},
		{"file:///w/b.sh", "echo\n", selectable(false, dialectAnswer{Dialect: "sh", Source: "extension", Evidence: ".sh", Guessed: true, Description: "guessed from the .sh extension"})},
		{"file:///w/run", "echo\n", selectable(false, dialectAnswer{Dialect: "bash", Source: "default", Guessed: true, Description: "guessed: nothing names the shell, so bash is assumed"})},
		{"file:///w/c.sh", "#!/bin/sh\n# shellcheck shell=dash\necho\n", selectable(true, dialectAnswer{Dialect: "dash", Source: "directive", Evidence: "dash", Description: "from the directive # shellcheck shell=dash"})},
	} {
		c.notify("textDocument/didOpen", openParams(tc.uri, tc.text))
		if got := askDialect(t, c, tc.uri); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %+v, want %+v", tc.uri, got, tc.want)
		}
	}
	if missing := c.call(documentDialectMethod, `{"textDocument": {"uri": "file:///w/closed.sh"}}`); missing.Error == nil {
		t.Error("a closed document got a dialect")
	}
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
