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

func TestHoverOnADisabledCodeCountsWhatItSilences(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}, "hover": {"contentFormat": ["markdown"]}}}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.sh", "#!/bin/bash\nx=$1\n# shellcheck disable=SC2086\necho $x\n"))
	got := decodeResult[lsp.Hover](t, c.call("textDocument/hover", positionParams("file:///w/a.sh", 2, 23, "")))
	for _, want := range []string{"**SC2086** · info", "Silences 1 finding here:", "line 4"} {
		if !strings.Contains(got.Contents.Value, want) {
			t.Errorf("hover lacks %q:\n%s", want, got.Contents.Value)
		}
	}

	off, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}, "hover": {"contentFormat": ["markdown"]}}}, "initializationOptions": {"lint": {"enable": false}}}`)
	off.notify("textDocument/didOpen", openParams("file:///w/a.sh", "#!/bin/bash\nx=$1\n# shellcheck disable=SC2086\necho $x\n"))
	if got := decodeResult[lsp.Hover](t, off.call("textDocument/hover", positionParams("file:///w/a.sh", 2, 23, ""))); strings.Contains(got.Contents.Value, "Silences") {
		t.Errorf("with lint off the hover still claims what the code silences:\n%s", got.Contents.Value)
	}
}

func TestHoverExplainsTheDiagnosticUnderTheCursor(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}, "hover": {"contentFormat": ["markdown"]}}}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.sh", "#!/bin/bash\nx=$1\necho $x\n"))
	diagnosed := pullDiagnostics(t, c, "file:///w/a.sh")
	got := decodeResult[lsp.Hover](t, c.call("textDocument/hover", positionParams("file:///w/a.sh", 2, 6, "")))
	for _, want := range []string{"**SC2086** · info", "```diff\n-echo $x\n+echo \"$x\"\n```", "docs/checks.md#sc2086"} {
		if !strings.Contains(got.Contents.Value, want) {
			t.Errorf("hover lacks %q:\n%s", want, got.Contents.Value)
		}
	}
	if len(diagnosed) == 0 || diagnosed[0].CodeDescription != nil {
		t.Errorf("diagnostics = %+v, want SC2086 without a codeDescription the client did not ask for", diagnosed)
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

func TestHoverDocumentsSubcommandsAndTheirFlags(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}, "hover": {"contentFormat": ["markdown"]}}}}`, withToolPages(t))
	c.notify("textDocument/didOpen", openParams("file:///w/a.sh", "tool stop --force\ntool extra -x\n"))
	for _, tc := range []struct {
		line, character int
		want            string
	}{
		{0, 6, "Stop units."},
		{0, 12, "kill them"},
		{1, 6, "tool-extra - extra widget tricks"},
		{1, 12, "do the extra trick"},
	} {
		got := decodeResult[lsp.Hover](t, c.call("textDocument/hover", positionParams("file:///w/a.sh", tc.line, tc.character, "")))
		if !strings.Contains(got.Contents.Value, tc.want) {
			t.Errorf("hover at %d:%d = %q, want %q", tc.line, tc.character, got.Contents.Value, tc.want)
		}
	}
}
