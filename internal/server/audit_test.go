package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

func TestLogTrace(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"trace": "verbose", "capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.call("textDocument/hover", `{"textDocument": {"uri": "file:///missing.sh"}, "position": {"line": 0, "character": 0}}`)
	traced := c.awaitNotification("$/logTrace", func(raw json.RawMessage) bool {
		return strings.Contains(string(raw), "textDocument/hover")
	})
	var params logTraceParams
	if err := json.Unmarshal(traced, &params); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(params.Message, "Received request 'textDocument/hover - (") || !strings.Contains(params.Verbose, "missing.sh") {
		t.Errorf("trace = %+v", params)
	}
	c.notify("$/setTrace", `{"value": "off"}`)
	c.call("shellLsp/none", "")
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, m := range c.messages {
		if m.Method == "$/logTrace" && strings.Contains(string(m.Params), "shellLsp/none") {
			t.Error("traced after $/setTrace off")
		}
	}
}

func TestProgressCancellation(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"window": {"workDoneProgress": true}}}`, func(srv *Server) {
		srv.requests["test/longScan"] = func(ctx context.Context, _ *docstore.View, _ json.RawMessage) (any, error) {
			progress, err := srv.StartProgress(ctx, "scanning")
			if err != nil {
				return nil, err
			}
			<-progress.Context().Done()
			return "stopped", progress.End("cancelled")
		}
	})
	id := c.start("test/longScan", "")
	created := c.answerServerRequest("window/workDoneProgress/create")
	var token progressCreateParams
	if err := json.Unmarshal(created, &token); err != nil {
		t.Fatal(err)
	}
	c.awaitNotification("$/progress", func(raw json.RawMessage) bool {
		return strings.Contains(string(raw), `"begin"`) && strings.Contains(string(raw), `"cancellable":true`)
	})
	c.notify("window/workDoneProgress/cancel", `{"token": "`+token.Token+`"}`)
	if got := decodeResult[string](t, c.awaitResponse(id, "test/longScan")); got != "stopped" {
		t.Errorf("result = %q", got)
	}
}

func TestRangesFormatting(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.sh", "f(){\necho  a\n}\ng(){\necho  b\n}\nh(){\necho  c\n}\n"))
	edits := decodeResult[[]lsp.TextEdit](t, c.call("textDocument/rangesFormatting",
		`{"textDocument": {"uri": "file:///w/a.sh"}, "ranges": [{"start": {"line": 1, "character": 0}, "end": {"line": 1, "character": 1}}, {"start": {"line": 7, "character": 0}, "end": {"line": 7, "character": 1}}], "options": {"tabSize": 2, "insertSpaces": true}}`))
	if len(edits) != 2 || edits[0].Range.Start.Line != 0 || edits[1].Range.Start.Line != 6 {
		t.Errorf("ranges formatting edits = %+v, want the hunks of f and h only", edits)
	}
}

func TestSettingsChangeRefreshesViews(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}, "workspace": {"semanticTokens": {"refreshSupport": true}, "inlayHint": {"refreshSupport": true}, "codeLens": {"refreshSupport": true}}}}`)
	c.notify("workspace/didChangeConfiguration", `{"settings": {"shellLsp": {"dialect": "bash"}}}`)
	for _, method := range []string{"workspace/semanticTokens/refresh", "workspace/inlayHint/refresh", "workspace/codeLens/refresh"} {
		c.answerServerRequest(method)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, m := range c.messages {
		if m.IsRequest() && m.Method == "workspace/diagnostic/refresh" {
			t.Error("diagnostic refresh sent to a client without refreshSupport")
		}
	}
}

func TestDollarRequestsAreMethodNotFound(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{}`)
	msg := c.call("$/unknownRequest", `{}`)
	if msg.Error == nil || msg.Error.Code != jsonrpc.CodeMethodNotFound {
		t.Errorf("$/ request error = %+v, want MethodNotFound", msg.Error)
	}
	c.notify("$/unknownNotification", `{}`)
}
