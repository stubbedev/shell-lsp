package server

import (
	"encoding/json"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

const pullCapabilities = `"capabilities": {"textDocument": {"diagnostic": {}}, "workspace": {"diagnostics": {"refreshSupport": true}}}`

func requireShellcheck(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("shellcheck"); err != nil {
		t.Skip("shellcheck not installed")
	}
}

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

func decodeResult[T any](t *testing.T, msg *jsonrpc.Message) T {
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

func TestPushDiagnosticsSyntaxAndShellcheck(t *testing.T) {
	t.Parallel()
	requireShellcheck(t)

	c, _ := startClient(t, `{"capabilities": {}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.sh", "#!/bin/bash\necho $1\nif true; then\n"))
	got := awaitPublished(c, "file:///w/a.sh", 1)
	isShellcheck := func(code string) bool { return strings.HasPrefix(code, "SC") }
	if got := codes(got.Diagnostics); !slices.Contains(got, "syntax") || !slices.ContainsFunc(got, isShellcheck) {
		t.Errorf("codes = %v, want our syntax error plus shellcheck findings", got)
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
	requireShellcheck(t)

	text := "#!/bin/sh\necho $1\narr=(1)\n"
	pull, _ := startClient(t, `{`+pullCapabilities+`}`)
	pull.notify("textDocument/didOpen", openParams("file:///w/p.sh", text))
	first := decodeResult[lsp.FullDocumentDiagnosticReport](t,
		pull.call("textDocument/diagnostic", `{"textDocument": {"uri": "file:///w/p.sh"}}`))
	if first.Kind != lsp.ReportFull || first.ResultID == "" {
		t.Fatalf("report = %+v", first)
	}

	push, _ := startClient(t, `{"capabilities": {}}`)
	push.notify("textDocument/didOpen", openParams("file:///w/p.sh", text))
	published := awaitPublished(push, "file:///w/p.sh", 1)
	if !slices.Equal(codes(first.Items), codes(published.Diagnostics)) {
		t.Errorf("pull codes %v != push codes %v", codes(first.Items), codes(published.Diagnostics))
	}

	again := decodeResult[lsp.UnchangedDocumentDiagnosticReport](t, pull.call("textDocument/diagnostic",
		`{"textDocument": {"uri": "file:///w/p.sh"}, "previousResultId": "`+first.ResultID+`"}`))
	if again.Kind != lsp.ReportUnchanged || again.ResultID != first.ResultID {
		t.Errorf("second report = %+v, want unchanged", again)
	}

	pull.notify("workspace/didChangeConfiguration", `{"settings": {"shellLsp": {"shellcheck": {"exclude": ["SC2086"]}}}}`)
	pull.answerServerRequest("workspace/diagnostic/refresh")
	after := decodeResult[lsp.FullDocumentDiagnosticReport](t, pull.call("textDocument/diagnostic",
		`{"textDocument": {"uri": "file:///w/p.sh"}, "previousResultId": "`+first.ResultID+`"}`))
	if after.Kind != lsp.ReportFull || slices.Contains(codes(after.Items), "SC2086") {
		t.Errorf("report after excluding SC2086 = %+v", after)
	}
	if !slices.Contains(codes(after.Items), "dialect") {
		t.Errorf("dialect error lost after configuration change: %v", codes(after.Items))
	}
}

func TestMissingShellcheckIsAnnouncedOnce(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{`+pullCapabilities+`, "initializationOptions": {"shellcheck": {"path": "shellcheck-not-installed-anywhere"}}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.sh", "echo $1\nif\n"))
	for range 2 {
		report := decodeResult[lsp.FullDocumentDiagnosticReport](t,
			c.call("textDocument/diagnostic", `{"textDocument": {"uri": "file:///w/a.sh"}}`))
		if got := codes(report.Items); len(got) == 0 || slices.ContainsFunc(got, func(code string) bool { return code != "syntax" }) {
			t.Errorf("codes = %v, want syntax errors only", got)
		}
	}
	notice := c.awaitNotification("window/logMessage", func(raw json.RawMessage) bool {
		return strings.Contains(string(raw), "shellcheck-not-installed-anywhere")
	})
	var params lsp.LogMessageParams
	if err := json.Unmarshal(notice, &params); err != nil || params.Type != lsp.MessageWarning {
		t.Errorf("notice = %s", notice)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, m := range c.messages {
		if m.Method == "window/logMessage" && strings.Contains(string(m.Params), "shellcheck-not-installed-anywhere") {
			t.Errorf("missing shellcheck announced more than once: %s", m.Params)
		}
	}
}

func TestZshSkipsShellcheckWithNotice(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{`+pullCapabilities+`}`)
	c.notify("textDocument/didOpen", openParams("file:///w/rc.zsh", "print -l *(.)\necho $1\n"))
	report := decodeResult[lsp.FullDocumentDiagnosticReport](t,
		c.call("textDocument/diagnostic", `{"textDocument": {"uri": "file:///w/rc.zsh"}}`))
	if len(report.Items) != 0 {
		t.Errorf("zsh diagnostics = %+v, want none", report.Items)
	}
	c.awaitNotification("window/logMessage", func(raw json.RawMessage) bool {
		return strings.Contains(string(raw), "does not support zsh")
	})
}

func TestDialectSettingOverridesDetection(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{`+pullCapabilities+`, "initializationOptions": {"dialect": "sh", "shellcheck": {"enable": false}}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.bash", "arr=(1 2)\n"))
	report := decodeResult[lsp.FullDocumentDiagnosticReport](t,
		c.call("textDocument/diagnostic", `{"textDocument": {"uri": "file:///w/a.bash"}}`))
	if got := codes(report.Items); !slices.Equal(got, []string{"dialect"}) {
		t.Errorf("codes = %v, want the POSIX dialect error", got)
	}
}

func TestInvalidInitializationOptionsFailInitialize(t *testing.T) {
	t.Parallel()

	srv := New(jsonrpc.NewConn(nil, nil), "test")
	_, err := srv.handleInitialize(t.Context(), json.RawMessage(`{"initializationOptions": {"dialect": "fish"}}`))
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
