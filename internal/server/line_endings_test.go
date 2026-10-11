package server

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/clienttext"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

func TestDiagnosticsPointAtClientTextWithEveryLineEnding(t *testing.T) {
	t.Parallel()

	const uri = "file:///w/endings.sh"
	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	text := "#!/bin/bash\recho $1\r\necho $2\recho $3\n"
	c.notify("textDocument/didOpen", openParams(uri, text))
	quoted := func(text string) []string {
		var got []string
		lines := clienttext.Lines(text)
		for _, d := range pullDiagnostics(t, c, uri) {
			if d.Code == "SC2086" {
				start := clienttext.Offset(text, lines, d.Range.Start, clienttext.UTF16)
				end := clienttext.Offset(text, lines, d.Range.End, clienttext.UTF16)
				got = append(got, text[start:end])
			}
		}
		slices.Sort(got)
		return got
	}
	if got := quoted(text); !slices.Equal(got, []string{"$1", "$2", "$3"}) {
		t.Fatalf("SC2086 ranges cover %q in the client's text, want $1, $2 and $3", got)
	}

	join := lsp.Position{Line: 3, Character: 0}
	change, err := json.Marshal(lsp.DidChangeTextDocumentParams{
		TextDocument:   lsp.VersionedTextDocumentIdentifier{URI: uri, Version: 2},
		ContentChanges: []lsp.TextDocumentContentChangeEvent{{Range: &lsp.Range{Start: join, End: join}, Text: "\necho $4\r"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	c.notify("textDocument/didChange", string(change))
	text = "#!/bin/bash\recho $1\r\necho $2\r\necho $4\recho $3\n"
	if got := quoted(text); !slices.Equal(got, []string{"$1", "$2", "$3", "$4"}) {
		t.Errorf("after joining a carriage return and a line feed, SC2086 ranges cover %q, want $1 to $4", got)
	}
}
