package server

import (
	"encoding/json"
	"errors"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

const pullCapabilities = `"capabilities": {"textDocument": {"diagnostic": {}}, "workspace": {"diagnostics": {"refreshSupport": true}}}`

const linkingCapabilities = `"capabilities": {"textDocument": {"diagnostic": {}, "publishDiagnostics": {"codeDescriptionSupport": true}}}`

func openParams(uri, text string) string {
	encoded, err := json.Marshal(lsp.DidOpenTextDocumentParams{TextDocument: lsp.TextDocumentItem{
		URI: uri, LanguageID: "shellscript", Version: 1, Text: text,
	}})
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func codes(items []lsp.Diagnostic) []string {
	out := make([]string, 0, len(items))
	for _, d := range items {
		out = append(out, d.Code)
	}
	slices.Sort(out)
	return out
}

func decodeResult[T any](t testing.TB, msg *jsonrpc.Message) T {
	t.Helper()

	if msg.Error != nil {
		t.Fatalf("request failed: %v", msg.Error)
	}
	var out T
	if err := json.Unmarshal(msg.Result, &out); err != nil {
		t.Fatalf("decoding result %s: %v", msg.Result, err)
	}
	return out
}

func awaitPublished(c *testClient, uri string, version int32) lsp.PublishDiagnosticsParams {
	c.t.Helper()

	var published lsp.PublishDiagnosticsParams
	c.awaitNotification("textDocument/publishDiagnostics", func(raw json.RawMessage) bool {
		var p lsp.PublishDiagnosticsParams
		if err := json.Unmarshal(raw, &p); err != nil || p.URI != uri {
			return false
		}
		if p.Version != nil && *p.Version != version {
			return false
		}
		published = p
		return true
	})
	return published
}

func TestPushDiagnosticsSyntaxAndLint(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.sh", "#!/bin/bash\necho $1\nif true; then\n"))
	if got := codes(awaitPublished(c, "file:///w/a.sh", 1).Diagnostics); !slices.Contains(got, "syntax") || !slices.Contains(got, "SC2086") {
		t.Errorf("publication = %v, want the syntax error and the unquoted expansion together", got)
	}

	c.notify("textDocument/didChange", `{"textDocument": {"uri": "file:///w/a.sh", "version": 2},
		"contentChanges": [{"text": "#!/bin/bash\necho \"$1\"\n"}]}`)
	cleared := awaitPublished(c, "file:///w/a.sh", 2)
	if len(cleared.Diagnostics) != 0 {
		t.Errorf("diagnostics after fix = %+v, want none", cleared.Diagnostics)
	}

	c.notify("textDocument/didClose", `{"textDocument": {"uri": "file:///w/a.sh"}}`)
	closed := c.awaitNotification("textDocument/publishDiagnostics", func(raw json.RawMessage) bool {
		return strings.Contains(string(raw), `"diagnostics":[]`) && !strings.Contains(string(raw), `"version"`)
	})
	if !strings.Contains(string(closed), "file:///w/a.sh") {
		t.Errorf("close notification = %s", closed)
	}
}

func TestPullDiagnosticsMatchesPushAndReportsUnchanged(t *testing.T) {
	t.Parallel()

	text := "#!/bin/bash\necho $1 `date`\n"
	pull, _ := startClient(t, `{`+pullCapabilities+`}`)
	pull.notify("textDocument/didOpen", openParams("file:///w/p.sh", text))
	first := decodeResult[lsp.FullDocumentDiagnosticReport](t,
		pull.call("textDocument/diagnostic", `{"textDocument": {"uri": "file:///w/p.sh"}}`))
	if first.Kind != lsp.ReportFull || !slices.Equal(codes(first.Items), []string{"SC2006", "SC2046", "SC2086"}) {
		t.Fatalf("first report = %+v", first)
	}

	push, _ := startClient(t, `{"capabilities": {}}`)
	push.notify("textDocument/didOpen", openParams("file:///w/p.sh", text))
	if published := awaitPublished(push, "file:///w/p.sh", 1); !slices.Equal(codes(first.Items), codes(published.Diagnostics)) {
		t.Errorf("pull codes %v != push codes %v", codes(first.Items), codes(published.Diagnostics))
	}

	again := decodeResult[lsp.UnchangedDocumentDiagnosticReport](t, pull.call("textDocument/diagnostic",
		`{"textDocument": {"uri": "file:///w/p.sh"}, "previousResultId": "`+first.ResultID+`"}`))
	if again.Kind != lsp.ReportUnchanged || again.ResultID != first.ResultID {
		t.Errorf("second report = %+v, want unchanged", again)
	}

	pull.notify("workspace/didChangeConfiguration", `{"settings": {"shellLsp": {"lint": {"exclude": ["SC2086"]}}}}`)
	pull.answerServerRequest("workspace/diagnostic/refresh")
	after := decodeResult[lsp.FullDocumentDiagnosticReport](t, pull.call("textDocument/diagnostic",
		`{"textDocument": {"uri": "file:///w/p.sh"}, "previousResultId": "`+first.ResultID+`"}`))
	if after.Kind != lsp.ReportFull || !slices.Equal(codes(after.Items), []string{"SC2006", "SC2046"}) {
		t.Errorf("report after excluding SC2086 = %+v", after)
	}
}

func TestZshIsLintedNatively(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{`+pullCapabilities+`}`)
	c.notify("textDocument/didOpen", openParams("file:///w/rc.zsh", "print -l *(.)\necho $1 `date`\n"))
	report := decodeResult[lsp.FullDocumentDiagnosticReport](t,
		c.call("textDocument/diagnostic", `{"textDocument": {"uri": "file:///w/rc.zsh"}}`))
	if got := codes(report.Items); !slices.Equal(got, []string{"SC2006", "SC2046"}) {
		t.Errorf("zsh codes = %v, want the backticks and the split substitution flagged but not the parameter", got)
	}
}

func TestForeignInterpreterGetsNoSyntaxErrors(t *testing.T) {
	t.Parallel()

	text := "#!/usr/bin/env python3\nimport sys\nCONF = Path.home() / \".conf\"\nprint(sys.argv)\n"
	c, _ := startClient(t, `{`+pullCapabilities+`}`)
	c.notify("textDocument/didOpen", openParams("file:///w/status.sh", text))
	report := decodeResult[lsp.FullDocumentDiagnosticReport](t,
		c.call("textDocument/diagnostic", `{"textDocument": {"uri": "file:///w/status.sh"}}`))
	if got := codes(report.Items); !slices.Equal(got, []string{"SC1071"}) {
		t.Errorf("python file codes = %v, want only SC1071", got)
	}

	forced, _ := startClient(t, `{`+pullCapabilities+`, "initializationOptions": {"dialect": "bash"}}`)
	forced.notify("textDocument/didOpen", openParams("file:///w/status.sh", text))
	report = decodeResult[lsp.FullDocumentDiagnosticReport](t,
		forced.call("textDocument/diagnostic", `{"textDocument": {"uri": "file:///w/status.sh"}}`))
	if got := codes(report.Items); !slices.Contains(got, "syntax") {
		t.Errorf("codes with the dialect forced = %v, want the file parsed as shell", got)
	}
}

func TestDialectSettingOverridesDetection(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{`+pullCapabilities+`, "initializationOptions": {"dialect": "zsh"}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.bash", "echo $1\n"))
	report := decodeResult[lsp.FullDocumentDiagnosticReport](t,
		c.call("textDocument/diagnostic", `{"textDocument": {"uri": "file:///w/a.bash"}}`))
	if len(report.Items) != 0 {
		t.Errorf("diagnostics = %+v, want none because zsh does not split unquoted parameters", report.Items)
	}
}

func TestInvalidInitializationOptionsFailInitialize(t *testing.T) {
	t.Parallel()

	srv := New(jsonrpc.NewConn(nil, nil), "test")
	_, err := srv.handleInitialize(t.Context(), nil, json.RawMessage(`{"initializationOptions": {"dialect": "fish"}}`))
	rpcErr, ok := errors.AsType[*jsonrpc.Error](err)
	if !ok || rpcErr.Code != jsonrpc.CodeInvalidParams || !strings.Contains(rpcErr.Message, "fish") {
		t.Errorf("error = %v, want InvalidParams naming the bad dialect", err)
	}
}

func TestInvalidConfigurationKeepsSettings(t *testing.T) {
	t.Parallel()

	c, srv := startClient(t, `{`+pullCapabilities+`, "initializationOptions": {"dialect": "zsh"}}`)
	c.notify("workspace/didChangeConfiguration", `{"settings": {"shellLsp": {"dialect": "fish"}}}`)
	c.awaitNotification("window/showMessage", func(raw json.RawMessage) bool {
		return strings.Contains(string(raw), "fish")
	})
	settings, _ := srv.currentSettings()
	if settings.Dialect == nil || settings.Dialect.String() != "zsh" {
		t.Errorf("settings changed by invalid configuration: %+v", settings)
	}
}

func TestPullForUnopenedDocumentIsInvalidParams(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{`+pullCapabilities+`}`)
	msg := c.call("textDocument/diagnostic", `{"textDocument": {"uri": "file:///never/opened.sh"}}`)
	if msg.Error == nil || msg.Error.Code != jsonrpc.CodeInvalidParams {
		t.Errorf("error = %+v, want InvalidParams", msg.Error)
	}
}

func TestWorkspaceDiagnosticReportsCarryItemsOnlyWhenFull(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFiles(t, root, map[string]string{"clean.sh": "#!/bin/sh\necho \"$1\"\n"})
	c, _ := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", `+pullCapabilities+`}`)

	type rawReports struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	first := decodeResult[rawReports](t, c.call("workspace/diagnostic", `{"previousResultIds": []}`))
	if len(first.Items) != 1 || string(first.Items[0]["kind"]) != `"full"` || string(first.Items[0]["items"]) != "[]" {
		t.Fatalf("first report = %s, want a full report with an empty items array", first.Items)
	}

	var resultID string
	if err := json.Unmarshal(first.Items[0]["resultId"], &resultID); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(lsp.WorkspaceDiagnosticParams{PreviousResultIDs: []lsp.PreviousResultID{{URI: fileuri.FromPath(filepath.Join(root, "clean.sh")), Value: resultID}}})
	if err != nil {
		t.Fatal(err)
	}
	second := decodeResult[rawReports](t, c.call("workspace/diagnostic", string(raw)))
	if len(second.Items) != 1 || string(second.Items[0]["kind"]) != `"unchanged"` {
		t.Fatalf("second report = %s, want an unchanged report", second.Items)
	}
	if items, ok := second.Items[0]["items"]; ok {
		t.Errorf("unchanged report carries items %s", items)
	}
}

func TestWorkspaceDiagnosticsCoverClosedFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"build.sh":     "#!/bin/bash\nrm $1\n",
		"lib/clean.sh": "#!/bin/sh\necho \"$1\"\n",
		"notes.txt":    "echo $1\n",
	})
	openURI := fileuri.FromPath(filepath.Join(root, "open.sh"))
	c, _ := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", `+pullCapabilities+`}`)
	c.notify("textDocument/didOpen", openParams(openURI, "#!/bin/bash\necho `date`\n"))

	first := decodeResult[lsp.WorkspaceDiagnosticReport](t, c.call("workspace/diagnostic", `{"previousResultIds": []}`))
	byURI := map[string]lsp.WorkspaceDocumentDiagnosticReport{}
	for _, item := range first.Items {
		byURI[item.URI] = item
	}
	build := byURI[fileuri.FromPath(filepath.Join(root, "build.sh"))]
	if build.Kind != lsp.ReportFull || build.Version != nil || !slices.Equal(codes(build.Items), []string{"SC2086"}) {
		t.Errorf("closed build.sh report = %+v", build)
	}
	if clean := byURI[fileuri.FromPath(filepath.Join(root, "lib", "clean.sh"))]; clean.Kind != lsp.ReportFull || len(clean.Items) != 0 {
		t.Errorf("closed clean.sh report = %+v", clean)
	}
	open := byURI[openURI]
	if open.Version == nil || *open.Version != 1 || !slices.Equal(codes(open.Items), []string{"SC2005", "SC2006", "SC2046"}) {
		t.Errorf("open document report = %+v", open)
	}
	if _, ok := byURI[fileuri.FromPath(filepath.Join(root, "notes.txt"))]; ok || len(first.Items) != 3 {
		t.Errorf("reported files = %v, want the three shell files only", slices.Collect(maps.Keys(byURI)))
	}

	previous := make([]lsp.PreviousResultID, 0, len(first.Items))
	for _, item := range first.Items {
		previous = append(previous, lsp.PreviousResultID{URI: item.URI, Value: item.ResultID})
	}
	raw, err := json.Marshal(lsp.WorkspaceDiagnosticParams{PreviousResultIDs: previous})
	if err != nil {
		t.Fatal(err)
	}
	second := decodeResult[lsp.WorkspaceDiagnosticReport](t, c.call("workspace/diagnostic", string(raw)))
	for _, item := range second.Items {
		if item.Kind != lsp.ReportUnchanged || len(item.Items) != 0 {
			t.Errorf("second report for %s = %+v, want unchanged", item.URI, item)
		}
	}
}
