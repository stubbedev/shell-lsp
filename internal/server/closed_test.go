package server

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/bats"
	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/perf"
)

func TestCachesDropWorkFinishingAfterClose(t *testing.T) {
	t.Parallel()

	const uri = "file:///closed.sh"
	srv := New(jsonrpc.NewConn(nil, nil), "test", CurrentProcess())
	srv.docs.Open(lsp.TextDocumentItem{URI: uri, LanguageID: "shellscript", Version: 1, Text: "echo hi\n"})
	stale := srv.docs.View()
	if err := srv.handleDidClose(t.Context(), json.RawMessage(`{"textDocument": {"uri": "`+uri+`"}}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.snapshot(stale, uri); err != nil {
		t.Fatalf("snapshot of a view taken before close: %v", err)
	}
	if _, err := srv.handleSemanticTokensFull(t.Context(), stale, json.RawMessage(`{"textDocument": {"uri": "`+uri+`"}}`)); err != nil {
		t.Fatalf("semantic tokens of a view taken before close: %v", err)
	}
	sizes := srv.cacheSizes()
	if sizes["snapshots"] != 0 || sizes["semantic"] != 0 {
		t.Errorf("caches kept a closed document: %v", sizes)
	}
}

func TestPushSchedulerForgetsJobsThatRan(t *testing.T) {
	t.Parallel()

	published := make(chan string)
	p := newPushScheduler(time.Millisecond, func(_ context.Context, uri string) { published <- uri })
	defer p.close()
	p.schedule("file:///closed.sh")
	<-published
	p.wg.Wait()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.jobs) != 0 {
		t.Errorf("jobs kept after they ran: %v", p.jobs)
	}
}

func TestUnansweredRefreshesDoNotPileUp(t *testing.T) {
	t.Parallel()

	c, srv := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}, "workspace": {"diagnostics": {"refreshSupport": true}, "semanticTokens": {"refreshSupport": true}}}}`)
	c.notify("initialized", `{}`)
	for i := range 50 {
		c.notify("workspace/didChangeConfiguration", fmt.Sprintf(`{"settings": {"shellLsp": {"lint": {"exclude": ["SC%d"]}}}}`, 2000+i))
	}
	for _, method := range []string{"workspace/diagnostic/refresh", "workspace/semanticTokens/refresh"} {
		c.await("server request "+method, func(m *jsonrpc.Message) bool { return m.IsRequest() && m.Method == method })
	}
	time.Sleep(100 * time.Millisecond)
	if n := syncMapLen(&srv.pending); n > 2 {
		t.Errorf("%d server requests wait for an answer, want at most one per refresh method", n)
	}
}

func TestBatsResultsDoNotPinTheDocument(t *testing.T) {
	t.Parallel()

	const uri = "file:///t.bats"
	results := newBatsResults()
	pinned := perf.PinsSource("helper() {\n  false\n}\n", func(src string) {
		tree := parser.Parse(src, dialect.Bash)
		results.record(uri, nil, []placedFailure{anchorFailure(tree, bats.Failure{Line: tree.Line(len(src) / 2)})})
	})
	if pinned {
		t.Error("a recorded failure keeps the document text its anchor was named from alive")
	}
	if failures := results.of(uri).failures; len(failures) != 1 || failures[0].anchor == nil {
		t.Errorf("failures = %+v, want one anchored to helper", failures)
	}
}
