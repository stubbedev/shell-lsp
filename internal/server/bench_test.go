package server

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/perf"
)

func BenchmarkEditCycle(b *testing.B) {
	root := b.TempDir()
	uri := fileuri.FromPath(filepath.Join(root, "big.sh"))
	c, _ := startClient(b, `{"rootUri": "`+fileuri.FromPath(root)+`", "initializationOptions": {"shellcheck": {"enable": false}}, "capabilities": {"textDocument": {"diagnostic": {}, "semanticTokens": {"requests": {"full": {"delta": true}}}}}}`)
	c.notify("textDocument/didOpen", openParams(uri, perf.LargeScript(10000)))
	doc := `{"textDocument": {"uri": "` + uri + `"}}`
	tokens := decodeResult[struct {
		ResultID string `json:"resultId"`
	}](b, c.call("textDocument/semanticTokens/full", doc))
	previous := tokens.ResultID
	version := 1
	for b.Loop() {
		version++
		c.notify("textDocument/didChange", fmt.Sprintf(`{"textDocument": {"uri": %q, "version": %d}, "contentChanges": [{"range": {"start": {"line": 2, "character": 2}, "end": {"line": 2, "character": 2}}, "text": "x"}]}`, uri, version))
		c.call("textDocument/diagnostic", doc)
		delta := decodeResult[struct {
			ResultID string `json:"resultId"`
		}](b, c.call("textDocument/semanticTokens/full/delta", fmt.Sprintf(`{"textDocument": {"uri": %q}, "previousResultId": %q}`, uri, previous)))
		previous = delta.ResultID
		c.call("textDocument/documentSymbol", doc)
		c.call("textDocument/foldingRange", doc)
		c.call("textDocument/inlayHint", `{"textDocument": {"uri": "`+uri+`"}, "range": {"start": {"line": 0, "character": 0}, "end": {"line": 120, "character": 0}}}`)
		c.call("textDocument/codeLens", doc)
		c.call("textDocument/documentHighlight", positionParams(uri, 3, 4, ""))
		c.call("textDocument/completion", positionParams(uri, 4, 3, ""))
	}
}
