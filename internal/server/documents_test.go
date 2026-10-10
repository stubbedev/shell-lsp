package server

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
)

func TestDocumentSync(t *testing.T) {
	t.Parallel()

	clientReader, clientWriter := io.Pipe()
	serverReader, serverWriter := io.Pipe()
	srv := New(jsonrpc.NewConn(serverReader, clientWriter), "test")

	srv.requests["test/document"] = func(_ context.Context, _ json.RawMessage) (any, error) {
		doc, ok := srv.docs.Document("file:///t.sh")
		if !ok {
			return nil, nil
		}
		return doc.Text(), nil
	}

	runDone := make(chan error, 1)
	go func() {
		runDone <- srv.Run(t.Context())
	}()

	client := jsonrpc.NewConn(clientReader, serverWriter)
	sendMsg := func(msg *jsonrpc.Message) {
		t.Helper()
		if err := client.Write(msg); err != nil {
			t.Fatalf("writing %s: %v", msg.Method, err)
		}
	}
	readResponse := func(id string) *jsonrpc.Message {
		t.Helper()
		for {
			msg, err := client.Read()
			if err != nil {
				t.Fatalf("reading response %s: %v", id, err)
			}
			if !msg.IsResponse() || string(msg.ID) != id {
				continue
			}
			return msg
		}
	}

	sendMsg(request(1, "initialize", `{"capabilities": {"general": {"positionEncodings": ["utf-32"]}}}`))
	initMsg := readResponse("1")
	var result InitializeResult
	if err := json.Unmarshal(initMsg.Result, &result); err != nil {
		t.Fatalf("parsing initialize result: %v", err)
	}
	if result.Capabilities.PositionEncoding != "utf-32" {
		t.Errorf("positionEncoding = %q, want utf-32", result.Capabilities.PositionEncoding)
	}
	if result.Capabilities.TextDocumentSync.Change != 2 {
		t.Errorf("textDocumentSync.change = %d, want incremental", result.Capabilities.TextDocumentSync.Change)
	}

	didOpen := notification("textDocument/didOpen")
	didOpen.Params = json.RawMessage(`{
		"textDocument": {"uri": "file:///t.sh", "languageId": "shellscript", "version": 1, "text": "\ufeff#!/bin/bash\r\necho héllo\r\n"}}`)
	sendMsg(didOpen)

	didChange := notification("textDocument/didChange")
	didChange.Params = json.RawMessage(`{
		"textDocument": {"uri": "file:///t.sh", "version": 2},
		"contentChanges": [{"range": {"start": {"line": 1, "character": 5}, "end": {"line": 1, "character": 11}}, "text": "wörld"}]}`)
	sendMsg(didChange)

	sendMsg(request(2, "test/document", ``))
	textMsg := readResponse("2")
	if textMsg.Error != nil {
		t.Fatalf("test/document failed: %v", textMsg.Error)
	}
	var seenText string
	if err := json.Unmarshal(textMsg.Result, &seenText); err != nil {
		t.Fatalf("parsing document text: %v", err)
	}
	if want := "#!/bin/bash\necho wörld\n"; seenText != want {
		t.Errorf("document text = %q, want %q", seenText, want)
	}

	didClose := notification("textDocument/didClose")
	didClose.Params = json.RawMessage(`{"textDocument": {"uri": "file:///t.sh"}}`)
	sendMsg(didClose)

	sendMsg(request(3, "test/document", ``))
	closedMsg := readResponse("3")
	var closed any
	if err := json.Unmarshal(closedMsg.Result, &closed); err != nil {
		t.Fatalf("parsing closed document result: %v", err)
	}
	if closed != nil {
		t.Errorf("document still present after didClose: %v", closed)
	}

	sendMsg(request(4, "shutdown", ``))
	readResponse("4")
	sendMsg(notification("exit"))
	if err := <-runDone; err != nil {
		t.Fatalf("Run() error = %v", err)
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
	doc, ok := srv.docs.Document("file:///t.sh")
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
	doc, ok := srv.docs.Document("file:///t.sh")
	if !ok {
		t.Fatal("document missing")
	}
	if doc.Text() != "two" || doc.Version != 2 {
		t.Errorf("after save then change: text=%q version=%d, want two/2", doc.Text(), doc.Version)
	}
}
