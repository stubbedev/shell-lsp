package server

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/semantic"
)

func applyTokenEdits(data []uint32, edits []lsp.SemanticTokensEdit) []uint32 {
	out := slices.Clone(data)
	for _, e := range slices.Backward(edits) {
		out = slices.Replace(out, int(e.Start), int(e.Start+e.DeleteCount), e.Data...)
	}
	return out
}

func TestSemanticTokens(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.bash", "x=1\necho \"😀$x\"\n"))
	full := decodeResult[lsp.SemanticTokens](t, c.call("textDocument/semanticTokens/full", `{"textDocument": {"uri": "file:///w/a.bash"}}`))
	want := []uint32{
		0, 0, 1, uint32(semantic.TypeVariable), uint32(semantic.ModModification),
		1, 0, 4, uint32(semantic.TypeFunction), uint32(semantic.ModDefaultLibrary),
		0, 5, 1, uint32(semantic.TypeString), 0,
		0, 1, 2, uint32(semantic.TypeString), 0,
		0, 3, 1, uint32(semantic.TypeVariable), 0,
		0, 1, 1, uint32(semantic.TypeString), 0,
	}
	if !slices.Equal(full.Data, want) || full.ResultID == "" {
		t.Errorf("full = %v (id %q)\nwant %v", full.Data, full.ResultID, want)
	}

	c.notify("textDocument/didChange", `{"textDocument": {"uri": "file:///w/a.bash", "version": 2}, "contentChanges": [{"text": "x=1\necho \"😀$x\"\nreadonly y=2\n"}]}`)
	delta := decodeResult[lsp.SemanticTokensDelta](t, c.call("textDocument/semanticTokens/full/delta", `{"textDocument": {"uri": "file:///w/a.bash"}, "previousResultId": "`+full.ResultID+`"}`))
	again := decodeResult[lsp.SemanticTokens](t, c.call("textDocument/semanticTokens/full", `{"textDocument": {"uri": "file:///w/a.bash"}}`))
	if got := applyTokenEdits(full.Data, delta.Edits); !slices.Equal(got, again.Data) || len(delta.Edits) != 1 {
		t.Errorf("delta %+v applied = %v, want %v", delta.Edits, got, again.Data)
	}

	stale := decodeResult[lsp.SemanticTokens](t, c.call("textDocument/semanticTokens/full/delta", `{"textDocument": {"uri": "file:///w/a.bash"}, "previousResultId": "unknown"}`))
	if !slices.Equal(stale.Data, again.Data) {
		t.Errorf("unknown previousResultId should answer with full tokens, got %+v", stale)
	}

	ranged := decodeResult[lsp.SemanticTokens](t, c.call("textDocument/semanticTokens/range", `{"textDocument": {"uri": "file:///w/a.bash"}, "range": {"start": {"line": 2, "character": 0}, "end": {"line": 3, "character": 0}}}`))
	if len(ranged.Data) != 10 || ranged.Data[0] != 2 {
		t.Errorf("range tokens = %v", ranged.Data)
	}
}

func TestSelectionRanges(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.sh", "echo \"$HOME\" | wc\n"))
	got := decodeResult[[]lsp.SelectionRange](t, c.call("textDocument/selectionRange", `{"textDocument": {"uri": "file:///w/a.sh"}, "positions": [{"line": 0, "character": 8}]}`))
	if len(got) != 1 {
		t.Fatalf("selection ranges = %+v", got)
	}
	depth := 0
	var prev *lsp.Range
	for r := &got[0]; r != nil; r = r.Parent {
		if prev != nil && (r.Range.Start.Character > prev.Start.Character || (r.Range.End.Line == prev.End.Line && r.Range.End.Character < prev.End.Character)) {
			t.Errorf("parent %+v does not contain child %+v", r.Range, *prev)
		}
		prev = &r.Range
		depth++
	}
	if depth < 5 || got[0].Range != (lsp.Range{Start: lsp.Position{Character: 7}, End: lsp.Position{Character: 11}}) {
		t.Errorf("innermost = %+v, depth %d", got[0].Range, depth)
	}
}
