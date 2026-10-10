package server

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

const actionCapabilities = `"capabilities": {"textDocument": {"diagnostic": {}, "codeAction": {"codeActionLiteralSupport": {}, "resolveSupport": {"properties": ["edit"]}}}, "workspace": {"workspaceEdit": {"documentChanges": true}}}`

func codeActions(t *testing.T, c *testClient, params string) []lsp.CodeAction {
	t.Helper()

	return decodeResult[[]lsp.CodeAction](t, c.call("textDocument/codeAction", params))
}

func titled(actions []lsp.CodeAction, prefix string) (lsp.CodeAction, bool) {
	i := slices.IndexFunc(actions, func(a lsp.CodeAction) bool { return strings.HasPrefix(a.Title, prefix) })
	if i < 0 {
		return lsp.CodeAction{}, false
	}
	return actions[i], true
}

func TestCodeActionsFromLint(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{`+actionCapabilities+`}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.sh", "#!/bin/bash\necho $1\n"))
	report := decodeResult[lsp.FullDocumentDiagnosticReport](t, c.call("textDocument/diagnostic", `{"textDocument": {"uri": "file:///w/a.sh"}}`))
	if len(report.Items) != 1 {
		t.Fatalf("diagnostics = %+v", report.Items)
	}
	diag, err := json.Marshal(report.Items)
	if err != nil {
		t.Fatal(err)
	}
	params := `{"textDocument": {"uri": "file:///w/a.sh"}, "range": {"start": {"line": 1, "character": 5}, "end": {"line": 1, "character": 5}}, "context": {"diagnostics": ` + string(diag) + `}}`
	got := codeActions(t, c, params)

	fix, ok := titled(got, "Fix SC2086")
	if !ok || !fix.IsPreferred || fix.Edit == nil || len(fix.Edit.DocumentChanges) != 1 || *fix.Edit.DocumentChanges[0].TextDocument.Version != 1 {
		t.Fatalf("fix action = %+v (all: %+v)", fix, got)
	}
	if edits := fix.Edit.DocumentChanges[0].Edits; len(edits) != 2 || edits[0].NewText != `"` {
		t.Errorf("fix edits = %+v", edits)
	}
	if len(fix.Diagnostics) != 1 || fix.Diagnostics[0].Code != "SC2086" {
		t.Errorf("fix diagnostics = %+v", fix.Diagnostics)
	}
	if _, ok := titled(got, "Ignore SC2086 in this file"); !ok {
		t.Error("file-level ignore missing")
	}
	if all, ok := titled(got, "Apply all automatic fixes"); !ok || all.Kind != "source.fixAll.shellLsp" {
		t.Errorf("fix all = %+v", all)
	}

	quickOnly := codeActions(t, c, strings.Replace(params, `"diagnostics": `, `"only": ["quickfix"], "diagnostics": `, 1))
	for _, a := range quickOnly {
		if a.Kind != "quickfix" {
			t.Errorf("only=quickfix returned %q", a.Kind)
		}
	}
}

func TestRefactorResolvesLazily(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{`+actionCapabilities+`}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.bash", "f() {\n\tcount=1\n}\n"))
	got := codeActions(t, c, `{"textDocument": {"uri": "file:///w/a.bash"}, "range": {"start": {"line": 1, "character": 2}, "end": {"line": 1, "character": 2}}, "context": {"diagnostics": []}}`)
	local, ok := titled(got, "Declare count as local")
	if !ok || local.Edit != nil || len(local.Data) == 0 {
		t.Fatalf("lazy action = %+v (all %+v)", local, got)
	}
	raw, err := json.Marshal(local)
	if err != nil {
		t.Fatal(err)
	}
	resolved := decodeResult[lsp.CodeAction](t, c.call("codeAction/resolve", string(raw)))
	if resolved.Edit == nil || resolved.Edit.DocumentChanges[0].Edits[0].NewText != "local " {
		t.Errorf("resolved = %+v", resolved)
	}

	c.notify("textDocument/didChange", `{"textDocument": {"uri": "file:///w/a.bash", "version": 2}, "contentChanges": [{"text": "x=1\n"}]}`)
	stale := c.call("codeAction/resolve", string(raw))
	if stale.Error == nil || stale.Error.Code != jsonrpc.CodeContentModified {
		t.Errorf("stale resolve error = %+v", stale.Error)
	}
}

func TestExecuteFixAllAppliesThroughClient(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{`+actionCapabilities+`}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.sh", "#!/bin/bash\necho $1 $2\n"))
	id := c.start("workspace/executeCommand", `{"command": "shellLsp.fixAll", "arguments": ["file:///w/a.sh"]}`)
	applied := c.await("workspace/applyEdit", func(m *jsonrpc.Message) bool { return m.IsRequest() && m.Method == "workspace/applyEdit" })
	var params lsp.ApplyWorkspaceEditParams
	if err := json.Unmarshal(applied.Params, &params); err != nil {
		t.Fatal(err)
	}
	if len(params.Edit.DocumentChanges) != 1 || len(params.Edit.DocumentChanges[0].Edits) != 4 {
		t.Errorf("applyEdit = %+v", params)
	}
	if err := c.conn.Reply(applied.ID, lsp.ApplyWorkspaceEditResult{Applied: true}, nil); err != nil {
		t.Fatal(err)
	}
	done := c.awaitResponse(id, "workspace/executeCommand")
	if done.Error != nil {
		t.Errorf("executeCommand error = %+v", done.Error)
	}

	unknown := c.call("workspace/executeCommand", `{"command": "nope"}`)
	if unknown.Error == nil || !strings.Contains(unknown.Error.Message, "shellLsp.fixAll") {
		t.Errorf("unknown command error = %+v", unknown.Error)
	}
}

func TestCodeActionsAsCommandsForLegacyClients(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.bash", "f() {\n\tcount=1\n}\n"))
	got := decodeResult[[]lsp.Command](t, c.call("textDocument/codeAction", `{"textDocument": {"uri": "file:///w/a.bash"}, "range": {"start": {"line": 1, "character": 2}, "end": {"line": 1, "character": 2}}, "context": {"diagnostics": []}}`))
	i := slices.IndexFunc(got, func(cmd lsp.Command) bool { return cmd.Title == "Declare count as local" })
	if i < 0 || got[i].Command != "shellLsp.applyEdit" || len(got[i].Arguments) != 1 {
		t.Errorf("commands = %+v", got)
	}
}
