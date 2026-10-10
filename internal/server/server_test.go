package server

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
)

func send(t *testing.T, input io.Writer, msgs ...*jsonrpc.Message) {
	t.Helper()

	conn := jsonrpc.NewConn(nil, input)
	for _, msg := range msgs {
		if err := conn.Write(msg); err != nil {
			t.Fatalf("writing %s: %v", msg.Method, err)
		}
	}
}

func request(id int, method string, params string) *jsonrpc.Message {
	msg := &jsonrpc.Message{
		JSONRPC: jsonrpc.Version,
		ID:      json.RawMessage(strconv.Itoa(id)),
		Method:  method,
	}
	if params != "" {
		msg.Params = json.RawMessage(params)
	}
	return msg
}

func notification(method string) *jsonrpc.Message {
	return &jsonrpc.Message{JSONRPC: jsonrpc.Version, Method: method}
}

func TestRunLifecycle(t *testing.T) {
	t.Parallel()

	var input bytes.Buffer
	var output bytes.Buffer
	srv := New(jsonrpc.NewConn(&input, &output), "test")

	send(t, &input,
		request(1, "initialize", `{"rootUri":"file:///ws"}`),
		request(2, "shutdown", ""),
		notification("exit"))

	if err := srv.Run(t.Context()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	client := jsonrpc.NewConn(&output, io.Discard)
	first, err := client.Read()
	if err != nil {
		t.Fatalf("reading initialize response: %v", err)
	}
	var result InitializeResult
	if err := json.Unmarshal(first.Result, &result); err != nil {
		t.Fatalf("parsing initialize result: %v", err)
	}
	if result.ServerInfo.Name != "shell-lsp" {
		t.Errorf("serverInfo.name = %q, want shell-lsp", result.ServerInfo.Name)
	}
	if result.Capabilities.TextDocumentSync.Change != 1 {
		t.Errorf("textDocumentSync.change = %d, want full sync", result.Capabilities.TextDocumentSync.Change)
	}
	second, err := client.Read()
	if err != nil {
		t.Fatalf("reading shutdown response: %v", err)
	}
	if string(second.Result) != "null" {
		t.Errorf("shutdown result = %s, want null", second.Result)
	}
}

func TestUnknownRequestMethod(t *testing.T) {
	t.Parallel()

	var input bytes.Buffer
	var output bytes.Buffer
	srv := New(jsonrpc.NewConn(&input, &output), "test")

	send(t, &input,
		request(9, "textDocument/hover", ""),
		notification("exit"))

	if err := srv.Run(t.Context()); err == nil {
		t.Fatal("Run() after exit-without-shutdown succeeded, want error")
	}

	client := jsonrpc.NewConn(&output, io.Discard)
	msg, err := client.Read()
	if err != nil {
		t.Fatalf("reading response: %v", err)
	}
	if msg.Error == nil || msg.Error.Code != jsonrpc.CodeMethodNotFound {
		t.Errorf("error = %+v, want method not found", msg.Error)
	}
}

func TestInvalidInitializeParams(t *testing.T) {
	t.Parallel()

	var input bytes.Buffer
	var output bytes.Buffer
	srv := New(jsonrpc.NewConn(&input, &output), "test")

	send(t, &input,
		request(1, "initialize", `{"processId":"not-a-number"}`),
		request(2, "shutdown", ""),
		notification("exit"))

	if err := srv.Run(t.Context()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	client := jsonrpc.NewConn(&output, io.Discard)
	msg, err := client.Read()
	if err != nil {
		t.Fatalf("reading response: %v", err)
	}
	if msg.Error == nil || msg.Error.Code != jsonrpc.CodeInvalidParams {
		t.Errorf("error = %+v, want invalid params", msg.Error)
	}
}

func TestEOFBeforeExit(t *testing.T) {
	t.Parallel()

	var input bytes.Buffer
	var output bytes.Buffer
	srv := New(jsonrpc.NewConn(&input, &output), "test")

	if err := srv.Run(t.Context()); err == nil {
		t.Error("Run() on immediate EOF succeeded, want error")
	}
}
