package server

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

func positionParams(uri string, line, character int, extra string) string {
	return `{"textDocument": {"uri": "` + uri + `"}, "position": {"line": ` + strconv.Itoa(line) + `, "character": ` + strconv.Itoa(character) + `}` + extra + `}`
}

func TestReferencesHighlightAndRename(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	lib := filepath.Join(root, "lib.sh")
	if err := os.WriteFile(lib, []byte("greet() { echo hi; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mainURI := fileuri.FromPath(filepath.Join(root, "main.sh"))
	c, _ := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", "capabilities": {"textDocument": {"diagnostic": {}}, "workspace": {"workspaceEdit": {"documentChanges": true}}}}`)
	c.notify("textDocument/didOpen", openParams(mainURI, "source ./lib.sh\nname=world\ngreet \"$name\"\nf() {\n\tlocal name=x\n\techo \"$name\"\n}\n"))

	refs := decodeResult[[]lsp.Location](t, c.call("textDocument/references", positionParams(mainURI, 2, 1, `, "context": {"includeDeclaration": true}`)))
	if len(refs) != 2 || refs[0].URI != fileuri.FromPath(lib) || refs[1].URI != mainURI {
		t.Errorf("greet references = %+v", refs)
	}

	highlights := decodeResult[[]lsp.DocumentHighlight](t, c.call("textDocument/documentHighlight", positionParams(mainURI, 1, 1, "")))
	if len(highlights) != 2 || highlights[0].Kind != lsp.HighlightWrite || highlights[1].Kind != lsp.HighlightRead || highlights[1].Range.Start.Line != 2 {
		t.Errorf("name highlights = %+v", highlights)
	}

	prepared := decodeResult[lsp.PrepareRenameResult](t, c.call("textDocument/prepareRename", positionParams(mainURI, 2, 2, "")))
	if prepared.Placeholder != "greet" || prepared.Range.Start != (lsp.Position{Line: 2}) {
		t.Errorf("prepareRename = %+v", prepared)
	}
	if none := c.call("textDocument/prepareRename", positionParams(mainURI, 0, 14, "")); none.Error != nil || string(none.Result) != "null" {
		t.Errorf("prepareRename on whitespace = %s / %v", none.Result, none.Error)
	}

	edit := decodeResult[lsp.WorkspaceEdit](t, c.call("textDocument/rename", positionParams(mainURI, 2, 2, `, "newName": "welcome"`)))
	if len(edit.DocumentChanges) != 2 {
		t.Fatalf("rename edit = %+v", edit)
	}
	for _, change := range edit.DocumentChanges {
		switch change.TextDocument.URI {
		case mainURI:
			if change.TextDocument.Version == nil || *change.TextDocument.Version != 1 {
				t.Errorf("open document edit not versioned: %+v", change.TextDocument)
			}
		case fileuri.FromPath(lib):
			if change.TextDocument.Version != nil {
				t.Errorf("closed file edit carries a version: %+v", change.TextDocument)
			}
		}
		if len(change.Edits) != 1 || change.Edits[0].NewText != "welcome" {
			t.Errorf("edits = %+v", change.Edits)
		}
	}

	bad := c.call("textDocument/rename", positionParams(mainURI, 1, 1, `, "newName": "not valid"`))
	if bad.Error == nil || bad.Error.Code != jsonrpc.CodeRequestFailed || !strings.Contains(bad.Error.Message, "not a valid variable name") {
		t.Errorf("invalid rename error = %+v", bad.Error)
	}
}
