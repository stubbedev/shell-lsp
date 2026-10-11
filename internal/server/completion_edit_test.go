package server

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

func TestCompletionReplacesTheWholeWord(t *testing.T) {
	t.Parallel()

	uri := fileuri.FromPath(filepath.Join(t.TempDir(), "main.sh"))
	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}, "completion": {"completionItem": {"insertReplaceSupport": true, "labelDetailsSupport": true}}}}}`)
	c.notify("textDocument/didOpen", openParams(uri, "greeting=hi\necho \"$grting\"\n[[ -f x ]]\n"))
	complete := func(line, character int) lsp.CompletionList {
		t.Helper()
		params, err := json.Marshal(lsp.CompletionParams{
			TextDocument: lsp.TextDocumentIdentifier{URI: uri},
			Position:     lsp.Position{Line: uint32(line), Character: uint32(character)},
		})
		if err != nil {
			t.Fatal(err)
		}
		return decodeResult[lsp.CompletionList](t, c.call("textDocument/completion", string(params)))
	}

	vars := complete(1, 9)
	i := slices.IndexFunc(vars.Items, func(it lsp.CompletionItem) bool { return it.Label == "greeting" })
	if i < 0 {
		t.Fatalf("variables = %v", completionLabels(vars))
	}
	edit := vars.Items[i].TextEdit
	insert := lsp.Range{Start: lsp.Position{Line: 1, Character: 7}, End: lsp.Position{Line: 1, Character: 9}}
	replace := lsp.Range{Start: lsp.Position{Line: 1, Character: 7}, End: lsp.Position{Line: 1, Character: 13}}
	if edit == nil || edit.Insert != insert || edit.Replace == nil || *edit.Replace != replace {
		t.Errorf("greeting edit = %+v", edit)
	}
	if vars.Items[i].LabelDetails == nil || vars.Items[i].LabelDetails.Description != "global" {
		t.Errorf("greeting label details = %+v", vars.Items[i].LabelDetails)
	}

	ops := complete(2, 5)
	j := slices.IndexFunc(ops.Items, func(it lsp.CompletionItem) bool { return it.Label == "-f" })
	if j < 0 || ops.Items[j].LabelDetails == nil || ops.Items[j].LabelDetails.Detail != " file" || ops.Items[j].Kind != lsp.CompletionOperator {
		t.Fatalf("operators = %+v", ops.Items)
	}
	if ops.Items[j].TextEdit.Replace != nil {
		t.Errorf("-f replaces past the cursor although the operator is complete: %+v", ops.Items[j].TextEdit)
	}

	raw, err := json.Marshal(vars.Items[i])
	if err != nil {
		t.Fatal(err)
	}
	resolved := decodeResult[lsp.CompletionItem](t, c.call("completionItem/resolve", string(raw)))
	if resolved.TextEdit == nil || resolved.TextEdit.Replace == nil || *resolved.TextEdit.Replace != replace {
		t.Errorf("resolve lost the insert-replace edit: %+v", resolved.TextEdit)
	}
}

func TestCompletionEditWithoutInsertReplaceSupport(t *testing.T) {
	t.Parallel()

	uri := fileuri.FromPath(filepath.Join(t.TempDir(), "main.sh"))
	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openParams(uri, "greeting=hi\necho \"$grting\"\n"))
	params, err := json.Marshal(lsp.CompletionParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: uri},
		Position:     lsp.Position{Line: 1, Character: 9},
	})
	if err != nil {
		t.Fatal(err)
	}
	list := decodeResult[struct {
		Items []map[string]json.RawMessage `json:"items"`
	}](t, c.call("textDocument/completion", string(params)))
	for _, item := range list.Items {
		var edit map[string]json.RawMessage
		if err := json.Unmarshal(item["textEdit"], &edit); err != nil {
			t.Fatal(err)
		}
		if _, ok := edit["range"]; !ok || edit["replace"] != nil || item["labelDetails"] != nil {
			t.Fatalf("item sent features the client did not announce: %s", item["textEdit"])
		}
	}
}
