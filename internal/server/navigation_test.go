package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

func TestDefinitionAcrossSourcedFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	lib := filepath.Join(root, "lib.sh")
	if err := os.WriteFile(lib, []byte("# helper\nhelper() { :; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mainURI := fileuri.FromPath(filepath.Join(root, "main.sh"))
	c, _ := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", "capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openParams(mainURI, "source ./lib.sh\nhelper\n"))

	request := func(method string, line, character uint32) []lsp.Location {
		t.Helper()
		params, err := json.Marshal(lsp.TextDocumentPositionParams{
			TextDocument: lsp.TextDocumentIdentifier{URI: mainURI},
			Position:     lsp.Position{Line: line, Character: character},
		})
		if err != nil {
			t.Fatal(err)
		}
		return decodeResult[[]lsp.Location](t, c.call(method, string(params)))
	}

	got := request("textDocument/definition", 1, 2)
	want := lsp.Location{URI: fileuri.FromPath(lib), Range: lsp.Range{Start: lsp.Position{Line: 1}, End: lsp.Position{Line: 1, Character: 6}}}
	if len(got) != 1 || got[0] != want {
		t.Errorf("definition = %+v, want %+v", got, want)
	}
	source := request("textDocument/definition", 0, 10)
	if len(source) != 1 || source[0].URI != fileuri.FromPath(lib) || source[0].Range != (lsp.Range{}) {
		t.Errorf("source definition = %+v", source)
	}
	if none := request("textDocument/typeDefinition", 1, 2); len(none) != 0 {
		t.Errorf("typeDefinition of a function = %+v, want empty", none)
	}

	if err := os.WriteFile(lib, []byte("\n\nhelper() { :; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.notify("workspace/didChangeWatchedFiles", `{"changes": [{"uri": "`+fileuri.FromPath(lib)+`", "type": 2}]}`)
	moved := request("textDocument/definition", 1, 2)
	if len(moved) != 1 || moved[0].Range.Start.Line != 2 {
		t.Errorf("definition after change = %+v, want line 2", moved)
	}
}
