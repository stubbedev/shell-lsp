package server

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
)

func TestDocumentSync(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"general": {"positionEncodings": ["utf-32"]}, "textDocument": {"diagnostic": {}}}}`,
		func(srv *Server) {
			srv.requests["test/document"] = func(_ context.Context, docs *docstore.View, _ json.RawMessage) (any, error) {
				doc, ok := docs.Document("file:///t.sh")
				if !ok {
					return nil, nil
				}
				return doc.Text(), nil
			}
		})

	c.notify("textDocument/didOpen", `{"textDocument": {"uri": "file:///t.sh", "languageId": "shellscript", "version": 1,
		"text": "\ufeff#!/bin/bash\r\necho héllo\r\n"}}`)
	c.notify("textDocument/didChange", `{"textDocument": {"uri": "file:///t.sh", "version": 2},
		"contentChanges": [{"range": {"start": {"line": 1, "character": 5}, "end": {"line": 1, "character": 11}}, "text": "wörld"}]}`)
	if got := decodeResult[string](t, c.call("test/document", "")); got != "#!/bin/bash\necho wörld\n" {
		t.Errorf("document text = %q", got)
	}

	c.notify("textDocument/didClose", `{"textDocument": {"uri": "file:///t.sh"}}`)
	if got := decodeResult[*string](t, c.call("test/document", "")); got != nil {
		t.Errorf("document still present after didClose: %q", *got)
	}
}

func TestInitializeNegotiatesEncodingAndSync(t *testing.T) {
	t.Parallel()

	srv := New(jsonrpc.NewConn(nil, nil), "test")
	result, err := srv.handleInitialize(t.Context(), nil, json.RawMessage(`{"capabilities": {"general": {"positionEncodings": ["utf-32", "utf-16"]}, "textDocument": {"diagnostic": {}}}}`))
	if err != nil {
		t.Fatalf("handleInitialize() error = %v", err)
	}
	caps := result.(InitializeResult).Capabilities
	if caps.PositionEncoding != "utf-32" || caps.TextDocumentSync.Change != 2 {
		t.Errorf("capabilities = %+v, want utf-32 and incremental sync", caps)
	}
}

func TestStaleChangeIsIgnored(t *testing.T) {
	t.Parallel()

	srv := New(jsonrpc.NewConn(nil, nil), "test")
	openParams := json.RawMessage(`{"textDocument": {"uri": "file:///t.sh", "languageId": "shellscript", "version": 3, "text": "one"}}`)
	if err := srv.handleDidOpen(t.Context(), openParams); err != nil {
		t.Fatalf("didOpen: %v", err)
	}
	stale := json.RawMessage(`{"textDocument": {"uri": "file:///t.sh", "version": 3},
		"contentChanges": [{"text": "rewritten"}]}`)
	if err := srv.handleDidChange(t.Context(), stale); err != nil {
		t.Fatalf("didChange: %v", err)
	}
	doc, ok := srv.docs.View().Document("file:///t.sh")
	if !ok {
		t.Fatal("document missing")
	}
	if doc.Text() != "one" {
		t.Errorf("stale change applied: %q", doc.Text())
	}
	if doc.Version != 3 {
		t.Errorf("version = %d, want 3", doc.Version)
	}
}

func TestDidSaveKeepsVersionSequence(t *testing.T) {
	t.Parallel()

	srv := New(jsonrpc.NewConn(nil, nil), "test")
	open := json.RawMessage(`{"textDocument": {"uri": "file:///t.sh", "languageId": "shellscript", "version": 1, "text": "one"}}`)
	if err := srv.handleDidOpen(t.Context(), open); err != nil {
		t.Fatalf("didOpen: %v", err)
	}
	save := json.RawMessage(`{"textDocument": {"uri": "file:///t.sh"}, "text": "one"}`)
	if err := srv.handleDidSave(t.Context(), save); err != nil {
		t.Fatalf("didSave: %v", err)
	}
	change := json.RawMessage(`{"textDocument": {"uri": "file:///t.sh", "version": 2}, "contentChanges": [{"text": "two"}]}`)
	if err := srv.handleDidChange(t.Context(), change); err != nil {
		t.Fatalf("didChange: %v", err)
	}
	doc, ok := srv.docs.View().Document("file:///t.sh")
	if !ok {
		t.Fatal("document missing")
	}
	if doc.Text() != "two" || doc.Version != 2 {
		t.Errorf("after save then change: text=%q version=%d, want two/2", doc.Text(), doc.Version)
	}
}

func TestRequestsSeeDocumentsAsOfDispatch(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`, func(srv *Server) {
		srv.requests["test/slow-read"] = func(_ context.Context, docs *docstore.View, _ json.RawMessage) (any, error) {
			<-release
			doc, ok := docs.Document("file:///t.sh")
			if !ok {
				return nil, nil
			}
			return doc.Text(), nil
		}
	})
	c.notify("textDocument/didOpen", openParams("file:///t.sh", "before"))
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.mu.Unlock()
	c.send(request(id, "test/slow-read", ""))
	c.notify("textDocument/didChange", `{"textDocument": {"uri": "file:///t.sh", "version": 2}, "contentChanges": [{"text": "after"}]}`)
	time.Sleep(50 * time.Millisecond)
	close(release)
	msg := c.await("slow read response", func(m *jsonrpc.Message) bool { return m.IsResponse() && string(m.ID) == strconv.Itoa(id) })
	if got := decodeResult[string](t, msg); got != "before" {
		t.Errorf("request saw %q, want the document as it was when the request arrived", got)
	}
}
