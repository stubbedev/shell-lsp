package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

func TestCodeLensAndCallHierarchy(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "lib.sh"), []byte("log() { echo \"$*\"; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	uri := fileuri.FromPath(filepath.Join(root, "main.sh"))
	c, _ := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", "capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openParams(uri, "source ./lib.sh\nNAME=x\nbuild() {\n\tlog \"$NAME\"\n}\nbuild\nbuild\n"))

	lenses := decodeResult[[]lsp.CodeLens](t, c.call("textDocument/codeLens", `{"textDocument": {"uri": "`+uri+`"}}`))
	if len(lenses) != 2 || lenses[0].Command != nil {
		t.Fatalf("lenses = %+v", lenses)
	}
	raw, err := json.Marshal(lenses[0])
	if err != nil {
		t.Fatal(err)
	}
	resolved := decodeResult[lsp.CodeLens](t, c.call("codeLens/resolve", string(raw)))
	if resolved.Command == nil || resolved.Command.Title != "2 references" || resolved.Command.Command != "shellLsp.showReferences" {
		t.Errorf("resolved lens = %+v", resolved)
	}
	refs := decodeResult[[]lsp.Location](t, c.call("workspace/executeCommand", `{"command": "shellLsp.showReferences", "arguments": [`+string(resolved.Command.Arguments[0])+`]}`))
	if len(refs) != 2 {
		t.Errorf("showReferences = %+v", refs)
	}

	items := decodeResult[[]lsp.CallHierarchyItem](t, c.call("textDocument/prepareCallHierarchy", positionParams(uri, 3, 2, "")))
	if len(items) != 1 || items[0].Name != "log" || items[0].URI != fileuri.FromPath(filepath.Join(root, "lib.sh")) {
		t.Fatalf("prepare = %+v", items)
	}
	rawItem, err := json.Marshal(items[0])
	if err != nil {
		t.Fatal(err)
	}
	incoming := decodeResult[[]lsp.CallHierarchyIncomingCall](t, c.call("callHierarchy/incomingCalls", `{"item": `+string(rawItem)+`}`))
	if len(incoming) != 1 || incoming[0].From.Name != "build" || len(incoming[0].FromRanges) != 1 || incoming[0].FromRanges[0].Start.Line != 3 {
		t.Errorf("incoming = %+v", incoming)
	}
	buildItem, err := json.Marshal(incoming[0].From)
	if err != nil {
		t.Fatal(err)
	}
	outgoing := decodeResult[[]lsp.CallHierarchyOutgoingCall](t, c.call("callHierarchy/outgoingCalls", `{"item": `+string(buildItem)+`}`))
	if len(outgoing) != 1 || outgoing[0].To.Name != "log" {
		t.Errorf("outgoing = %+v", outgoing)
	}
}

func TestInlineValueMonikerAndLinkedEditing(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.bash", "f() {\n\tlocal n=1\n\techo \"$n\"\n}\ncat <<EOF\nx\nEOF\n"))

	values := decodeResult[[]lsp.InlineValueVariableLookup](t, c.call("textDocument/inlineValue", `{"textDocument": {"uri": "file:///w/a.bash"}, "range": {"start": {"line": 0, "character": 0}, "end": {"line": 4, "character": 0}}, "context": {"frameId": 1, "stoppedLocation": {"start": {"line": 2, "character": 1}, "end": {"line": 2, "character": 5}}}}`))
	if len(values) != 1 || values[0].VariableName != "n" || !values[0].CaseSensitiveLookup {
		t.Errorf("inline values = %+v", values)
	}

	monikers := decodeResult[[]lsp.Moniker](t, c.call("textDocument/moniker", positionParams("file:///w/a.bash", 0, 0, "")))
	if len(monikers) != 1 || monikers[0].Kind != "export" || monikers[0].Unique != "project" || monikers[0].Scheme != "shell-lsp" {
		t.Errorf("monikers = %+v", monikers)
	}

	linked := decodeResult[lsp.LinkedEditingRanges](t, c.call("textDocument/linkedEditingRange", positionParams("file:///w/a.bash", 4, 7, "")))
	if len(linked.Ranges) != 2 || linked.Ranges[1].Start.Line != 6 {
		t.Errorf("linked editing = %+v", linked)
	}
	if none := c.call("textDocument/linkedEditingRange", positionParams("file:///w/a.bash", 1, 2, "")); string(none.Result) != "null" {
		t.Errorf("linked editing outside heredoc = %s", none.Result)
	}
}
