package server

import (
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/lsp"
)

func TestHoverRequest(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"general": {"positionEncodings": ["utf-16"]}, "textDocument": {"diagnostic": {}, "hover": {"contentFormat": ["markdown"]}}}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.sh", "#!/bin/bash\n# Says hi.\nhi() { echo \"😀 $1\"; }\nhi there\n"))
	got := decodeResult[lsp.Hover](t, c.call("textDocument/hover", `{"textDocument": {"uri": "file:///w/a.sh"}, "position": {"line": 3, "character": 1}}`))
	if got.Contents.Kind != lsp.MarkupMarkdown || !strings.Contains(got.Contents.Value, "hi $1") || !strings.Contains(got.Contents.Value, "Says hi.") {
		t.Errorf("hover = %+v", got)
	}
	want := lsp.Range{Start: lsp.Position{Line: 3, Character: 0}, End: lsp.Position{Line: 3, Character: 2}}
	if got.Range == nil || *got.Range != want {
		t.Errorf("range = %+v, want %+v", got.Range, want)
	}

	param := decodeResult[lsp.Hover](t, c.call("textDocument/hover", `{"textDocument": {"uri": "file:///w/a.sh"}, "position": {"line": 2, "character": 17}}`))
	if !strings.Contains(param.Contents.Value, "argument 1 of function `hi`") {
		t.Errorf("hover after emoji = %+v (utf-16 position mapping)", param)
	}

	none := c.call("textDocument/hover", `{"textDocument": {"uri": "file:///w/a.sh"}, "position": {"line": 0, "character": 3}}`)
	if none.Error != nil || string(none.Result) != "null" {
		t.Errorf("hover on comment = %s / %v, want null", none.Result, none.Error)
	}
}

func TestHoverPlainTextClient(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}, "hover": {"contentFormat": ["plaintext"]}}}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.sh", "cd /tmp\n"))
	got := decodeResult[lsp.Hover](t, c.call("textDocument/hover", `{"textDocument": {"uri": "file:///w/a.sh"}, "position": {"line": 0, "character": 0}}`))
	if got.Contents.Kind != lsp.MarkupPlainText || !strings.Contains(got.Contents.Value, "Changes the working directory") {
		t.Errorf("hover = %+v", got)
	}
}
