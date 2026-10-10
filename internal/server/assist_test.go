package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

func TestSignatureHelpRequest(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.bash", "# Greets.\n# $1 - who\ngreet() { local who=\"$1\"; echo \"😀 $who\"; }\ngreet bob \n"))
	got := decodeResult[lsp.SignatureHelp](t, c.call("textDocument/signatureHelp", positionParams("file:///w/a.bash", 3, 6, "")))
	if len(got.Signatures) != 1 || got.Signatures[0].Label != "greet who" || got.ActiveParameter == nil || *got.ActiveParameter != 0 {
		t.Fatalf("signature help = %+v", got)
	}
	param := got.Signatures[0].Parameters[0]
	if param.Label != [2]uint32{6, 9} || param.Documentation == nil || param.Documentation.Value != "who" {
		t.Errorf("parameter = %+v", param)
	}
	beyond := decodeResult[lsp.SignatureHelp](t, c.call("textDocument/signatureHelp", positionParams("file:///w/a.bash", 3, 10, "")))
	if beyond.ActiveParameter != nil {
		t.Errorf("active parameter past the last one = %d", *beyond.ActiveParameter)
	}
	none := c.call("textDocument/signatureHelp", positionParams("file:///w/a.bash", 0, 3, ""))
	if string(none.Result) != "null" {
		t.Errorf("signature help in comment = %s", none.Result)
	}
}

func TestInlayHintsAndLinks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "lib.sh"), []byte("x=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	uri := fileuri.FromPath(filepath.Join(root, "main.bash"))
	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openParams(uri, "declare -a list\nhere=\"${BASH_SOURCE%/*}\"\nsource \"$here/lib.sh\"\n# see https://www.gnu.org/software/bash/\n"))

	hints := decodeResult[[]lsp.InlayHint](t, c.call("textDocument/inlayHint", `{"textDocument": {"uri": "`+uri+`"}, "range": {"start": {"line": 0, "character": 0}, "end": {"line": 9, "character": 0}}}`))
	if len(hints) != 2 || hints[0].Label != ": array" || hints[0].Kind != lsp.InlayHintType || hints[1].Label != "→ lib.sh" {
		t.Fatalf("hints = %+v", hints)
	}
	raw, err := json.Marshal(hints[1])
	if err != nil {
		t.Fatal(err)
	}
	resolved := decodeResult[lsp.InlayHint](t, c.call("inlayHint/resolve", string(raw)))
	if resolved.Tooltip == nil || !strings.HasSuffix(resolved.Tooltip.Value, "/lib.sh") {
		t.Errorf("resolved hint = %+v", resolved)
	}

	links := decodeResult[[]lsp.DocumentLink](t, c.call("textDocument/documentLink", `{"textDocument": {"uri": "`+uri+`"}}`))
	if len(links) != 2 || links[0].Target != fileuri.FromPath(filepath.Join(root, "lib.sh")) || links[1].Target != "https://www.gnu.org/software/bash/" {
		t.Errorf("links = %+v", links)
	}
}
