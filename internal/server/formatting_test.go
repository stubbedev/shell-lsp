package server

import (
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

const unformatted = "f(){\necho  a\n}\ng(){\necho  b\n}\n"

func TestFormatting(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.sh", unformatted))
	edits := decodeResult[[]lsp.TextEdit](t, c.call("textDocument/formatting",
		`{"textDocument": {"uri": "file:///w/a.sh"}, "options": {"tabSize": 2, "insertSpaces": true}}`))
	var newTexts []string
	for _, e := range edits {
		newTexts = append(newTexts, e.NewText)
	}
	joined := strings.Join(newTexts, "")
	if !strings.Contains(joined, "  echo a\n") || !strings.Contains(joined, "f() {\n") {
		t.Errorf("edits = %+v", edits)
	}
	if len(edits) != 2 {
		t.Errorf("got %d edits, want one hunk per function", len(edits))
	}

	ranged := decodeResult[[]lsp.TextEdit](t, c.call("textDocument/rangeFormatting",
		`{"textDocument": {"uri": "file:///w/a.sh"}, "range": {"start": {"line": 4, "character": 0}, "end": {"line": 4, "character": 3}}, "options": {"tabSize": 4, "insertSpaces": false}}`))
	if len(ranged) != 1 || ranged[0].Range.Start.Line != 3 || !strings.Contains(ranged[0].NewText, "\techo b") {
		t.Errorf("range edits = %+v, want only the hunk of g", ranged)
	}
}

func TestFormattingErrors(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}, "initializationOptions": {"format": {"indent": 3}}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/broken.sh", "if true; then\n"))
	msg := c.call("textDocument/formatting", `{"textDocument": {"uri": "file:///w/broken.sh"}, "options": {"tabSize": 2, "insertSpaces": true}}`)
	if msg.Error == nil || msg.Error.Code != jsonrpc.CodeRequestFailed || !strings.Contains(msg.Error.Message, "syntax errors") {
		t.Errorf("error = %+v", msg.Error)
	}

	c.notify("textDocument/didOpen", openParams("file:///w/ok.sh", unformatted))
	edits := decodeResult[[]lsp.TextEdit](t, c.call("textDocument/formatting",
		`{"textDocument": {"uri": "file:///w/ok.sh"}, "options": {"tabSize": 2, "insertSpaces": true}}`))
	if len(edits) == 0 || !strings.Contains(edits[0].NewText, "   echo a") {
		t.Errorf("configured indent ignored: %+v", edits)
	}

	c.notify("workspace/didChangeConfiguration", `{"settings": {"shellLsp": {"format": {"enable": false}}}}`)
	disabled := c.call("textDocument/formatting", `{"textDocument": {"uri": "file:///w/ok.sh"}, "options": {"tabSize": 2, "insertSpaces": true}}`)
	if disabled.Error == nil || !strings.Contains(disabled.Error.Message, "disabled") {
		t.Errorf("disabled formatting error = %+v", disabled.Error)
	}
}

func TestFormattingNixMatchesWholeFileInRanges(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	const uri = "file:///w/default.nix"
	c.notify("textDocument/didOpen", openParams(uri, "{\n  a=1;\n\n  b=2;\n}\n"))
	whole := decodeResult[[]lsp.TextEdit](t, c.call("textDocument/formatting",
		`{"textDocument": {"uri": "`+uri+`"}, "options": {"tabSize": 8, "insertSpaces": false}}`))
	if len(whole) != 2 || whole[0].NewText != "  a = 1;\n" || whole[1].NewText != "  b = 2;\n" {
		t.Fatalf("whole-file edits = %+v", whole)
	}
	ranged := decodeResult[[]lsp.TextEdit](t, c.call("textDocument/rangeFormatting",
		`{"textDocument": {"uri": "`+uri+`"}, "range": {"start": {"line": 3, "character": 0}, "end": {"line": 3, "character": 3}}, "options": {"tabSize": 8, "insertSpaces": false}}`))
	if len(ranged) != 1 || ranged[0] != whole[1] {
		t.Errorf("range edits = %+v, want only the whole-file edit of b: %+v", ranged, whole[1])
	}
}

func TestFormattingNixRefusesSyntaxErrors(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	const uri = "file:///w/broken.nix"
	c.notify("textDocument/didOpen", openParams(uri, "{ a = ; }\n"))
	resp := c.call("textDocument/formatting", `{"textDocument": {"uri": "`+uri+`"}, "options": {"tabSize": 2, "insertSpaces": true}}`)
	if resp.Error == nil || !strings.Contains(resp.Error.Message, "1:7") {
		t.Errorf("formatting a broken Nix file = %+v, want an error with its position", resp)
	}
}
