package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/docstore"
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

func readResponse(t *testing.T, client *jsonrpc.Conn) *jsonrpc.Message {
	t.Helper()

	msg, err := client.Read()
	if err != nil {
		t.Fatalf("reading response: %v", err)
	}
	return msg
}

func readerFor(output *bytes.Buffer) *jsonrpc.Conn {
	return jsonrpc.NewConn(output, io.Discard)
}

func responseByID(t *testing.T, client *jsonrpc.Conn, id string, count int) *jsonrpc.Message {
	t.Helper()

	for range count {
		msg, err := client.Read()
		if err != nil {
			t.Fatalf("reading response (wanted id %s): %v", id, err)
		}
		if string(msg.ID) == id {
			return msg
		}
	}
	t.Fatalf("no response with id %s", id)
	return nil
}

type lockedBuffer struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    bytes.Buffer
	closed bool
}

func newLockedBuffer() *lockedBuffer {
	b := &lockedBuffer{}
	b.cond = sync.NewCond(&b.mu)
	return b
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return 0, io.ErrClosedPipe
	}
	n, err := b.buf.Write(p)
	b.cond.Broadcast()
	return n, err
}

func (b *lockedBuffer) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for b.buf.Len() == 0 && !b.closed {
		b.cond.Wait()
	}
	return b.buf.Read(p)
}

func (b *lockedBuffer) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.cond.Broadcast()
	return nil
}

func TestRunLifecycle(t *testing.T) {
	t.Parallel()

	var input bytes.Buffer
	var output bytes.Buffer
	srv := New(jsonrpc.NewConn(&input, &output), "test", CurrentProcess())

	send(t, &input,
		request(1, "initialize", `{"rootUri":"file:///ws"}`),
		request(2, "shutdown", ""),
		notification("exit"))

	if err := srv.Run(t.Context()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	client := readerFor(&output)
	first := readResponse(t, client)
	var result InitializeResult
	if err := json.Unmarshal(first.Result, &result); err != nil {
		t.Fatalf("parsing initialize result: %v", err)
	}
	if result.ServerInfo.Name != "shell-lsp" {
		t.Errorf("serverInfo.name = %q, want shell-lsp", result.ServerInfo.Name)
	}
	if result.Capabilities.TextDocumentSync.Change != 2 {
		t.Errorf("textDocumentSync.change = %d, want incremental sync", result.Capabilities.TextDocumentSync.Change)
	}
	second := readResponse(t, client)
	if string(second.Result) != "null" {
		t.Errorf("shutdown result = %s, want null", second.Result)
	}
}

func TestUnknownRequestMethod(t *testing.T) {
	t.Parallel()

	var input bytes.Buffer
	var output bytes.Buffer
	srv := New(jsonrpc.NewConn(&input, &output), "test", CurrentProcess())

	send(t, &input,
		request(1, "initialize", `{}`),
		request(9, "shellLsp/noSuchMethod", ""),
		request(2, "shutdown", ""),
		notification("exit"))

	if err := srv.Run(t.Context()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	client := readerFor(&output)
	msg := responseByID(t, client, "9", 3)
	if msg.Error == nil || msg.Error.Code != jsonrpc.CodeMethodNotFound {
		t.Errorf("error = %+v, want method not found", msg.Error)
	}
}

func TestRequestBeforeInitialize(t *testing.T) {
	t.Parallel()

	var input bytes.Buffer
	var output bytes.Buffer
	srv := New(jsonrpc.NewConn(&input, &output), "test", CurrentProcess())

	opened := notification("textDocument/didOpen")
	opened.Params = json.RawMessage(`{"textDocument": {"uri": "file:///t.sh", "languageId": "shellscript", "version": 1, "text": "x"}}`)
	send(t, &input,
		request(9, "textDocument/hover", ""),
		request(1, "shutdown", ""),
		opened,
		notification("exit"))

	if err := srv.Run(t.Context()); err == nil {
		t.Error("exit without a successful shutdown returned no error (exit code 0)")
	}

	client := readerFor(&output)
	for _, id := range []string{"9", "1"} {
		msg := responseByID(t, client, id, 2)
		if msg.Error == nil || msg.Error.Code != jsonrpc.CodeServerNotInitialized {
			t.Errorf("response %s error = %+v, want ServerNotInitialized", id, msg.Error)
		}
	}
	if _, ok := srv.docs.View().Document("file:///t.sh"); ok {
		t.Error("didOpen before initialize was processed")
	}
}

func TestDoubleInitialize(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{}`)
	again := c.call("initialize", `{}`)
	if again.Error == nil || again.Error.Code != jsonrpc.CodeInvalidRequest {
		t.Errorf("second initialize error = %+v, want InvalidRequest", again.Error)
	}
}

func TestInvalidInitializeParams(t *testing.T) {
	t.Parallel()

	var input bytes.Buffer
	var output bytes.Buffer
	srv := New(jsonrpc.NewConn(&input, &output), "test", CurrentProcess())

	send(t, &input,
		request(1, "initialize", `{"processId":"not-a-number"}`),
		request(2, "initialize", `{}`),
		request(3, "shutdown", ""),
		notification("exit"))

	if err := srv.Run(t.Context()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	client := readerFor(&output)
	msg := readResponse(t, client)
	if msg.Error == nil || msg.Error.Code != jsonrpc.CodeInvalidParams {
		t.Errorf("error = %+v, want invalid params", msg.Error)
	}
	if retry := readResponse(t, client); retry.Error != nil {
		t.Errorf("initialize after a failed initialize = %+v, want success", retry.Error)
	}
}

func TestEOFBeforeExit(t *testing.T) {
	t.Parallel()

	var input bytes.Buffer
	var output bytes.Buffer
	srv := New(jsonrpc.NewConn(&input, &output), "test", CurrentProcess())

	if err := srv.Run(t.Context()); err == nil {
		t.Error("Run() on immediate EOF succeeded, want error")
	}
}

func TestNotificationsProcessedInOrder(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var order []string
	srv := New(jsonrpc.NewConn(nil, nil), "test", CurrentProcess())
	srv.initialized = true
	for _, name := range []string{"a", "b", "c"} {
		srv.notifications["test/"+name] = func(_ context.Context, _ json.RawMessage) error {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, name)
			return nil
		}
	}

	for _, name := range []string{"a", "b", "c"} {
		srv.dispatchNotification(t.Context(), notification("test/"+name))
	}

	want := []string{"a", "b", "c"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("notification order = %v, want %v", order, want)
		}
	}
}

func TestRequestsRunConcurrently(t *testing.T) {
	t.Parallel()

	input := newLockedBuffer()
	var output bytes.Buffer
	srv := New(jsonrpc.NewConn(input, &output), "test", CurrentProcess())

	started := make(chan string, 2)
	release := make(chan struct{})
	srv.requests["test/block"] = func(ctx context.Context, _ *docstore.View, _ json.RawMessage) (any, error) {
		started <- "ready"
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, nil
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		_ = srv.Run(ctx)
	}()

	client := jsonrpc.NewConn(nil, input)
	if err := client.Request(json.RawMessage("1"), "initialize", nil); err != nil {
		t.Fatalf("writing initialize: %v", err)
	}
	for id := range 2 {
		if err := client.Request(json.RawMessage(strconv.Itoa(id+2)), "test/block", nil); err != nil {
			t.Fatalf("writing request: %v", err)
		}
	}

	for range 2 {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			client := readerFor(&output)
			for range 5 {
				msg, err := client.Read()
				if err != nil {
					break
				}
				t.Logf("response id=%s method= error=%v result=%s", msg.ID, msg.Error, msg.Result)
			}
			t.Fatal("second request never started; requests are not concurrent")
		}
	}
	close(release)
	cancel()
	_ = input.Close()
}

func TestCancelRequest(t *testing.T) {
	t.Parallel()

	var input bytes.Buffer
	var output bytes.Buffer
	srv := New(jsonrpc.NewConn(&input, &output), "test", CurrentProcess())

	srv.requests["test/slow"] = func(ctx context.Context, _ *docstore.View, _ json.RawMessage) (any, error) {
		select {
		case <-time.After(2 * time.Second):
			return "done", nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	cancelMsg := notification("$/cancelRequest")
	cancelMsg.Params = json.RawMessage(`{"id":2}`)
	send(t, &input,
		request(1, "initialize", `{}`),
		request(2, "test/slow", ``),
		cancelMsg,
		request(3, "shutdown", ``),
		notification("exit"))

	if err := srv.Run(t.Context()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	client := readerFor(&output)
	msg := responseByID(t, client, "2", 3)
	if msg.Error == nil || msg.Error.Code != jsonrpc.CodeRequestCancelled {
		t.Errorf("error = %+v, want request cancelled", msg.Error)
	}
}

func TestServerInitiatedRequest(t *testing.T) {
	t.Parallel()

	clientReader, clientWriter := io.Pipe()
	serverReader, serverWriter := io.Pipe()
	srv := New(jsonrpc.NewConn(serverReader, clientWriter), "test", CurrentProcess())

	srv.requests["test/roundtrip"] = func(ctx context.Context, _ *docstore.View, params json.RawMessage) (any, error) {
		var result string
		if err := srv.call(ctx, "client/ping", map[string]string{"hello": "world"}, &result); err != nil {
			return nil, err
		}
		return result, nil
	}

	runErr := make(chan error, 1)
	go func() {
		runErr <- srv.Run(t.Context())
	}()
	go func() {
		send(t, serverWriter,
			request(1, "initialize", `{}`),
			request(2, "test/roundtrip", `{}`))
	}()

	client := jsonrpc.NewConn(clientReader, serverWriter)
	var roundtripResult string
	for roundtripResult == "" {
		msg, err := client.Read()
		if err != nil {
			t.Fatalf("client read: %v", err)
		}
		if msg.IsRequest() && msg.Method == "client/ping" {
			if err := client.Reply(msg.ID, "pong", nil); err != nil {
				t.Fatalf("client reply: %v", err)
			}
			continue
		}
		if msg.IsResponse() && string(msg.ID) == "2" {
			if msg.Error != nil {
				t.Fatalf("roundtrip failed: %v", msg.Error)
			}
			if err := json.Unmarshal(msg.Result, &roundtripResult); err != nil {
				t.Fatalf("parsing roundtrip result: %v", err)
			}
		}
	}
	if roundtripResult != "pong" {
		t.Errorf("roundtrip result = %q, want pong", roundtripResult)
	}

	go func() {
		for {
			if _, err := client.Read(); err != nil {
				return
			}
		}
	}()
	go func() {
		send(t, serverWriter, request(3, "shutdown", ``), notification("exit"))
	}()
	if err := <-runErr; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestProgressNotifications(t *testing.T) {
	t.Parallel()

	clientReader, clientWriter := io.Pipe()
	serverReader, serverWriter := io.Pipe()
	srv := New(jsonrpc.NewConn(serverReader, clientWriter), "test", CurrentProcess())

	srv.requests["test/progress"] = func(ctx context.Context, _ *docstore.View, _ json.RawMessage) (any, error) {
		progress, err := srv.StartProgress(ctx, "scanning workspace")
		if err != nil {
			return nil, err
		}
		if err := progress.Report("halfway", 50); err != nil {
			return nil, err
		}
		return nil, progress.End("done")
	}

	go func() {
		_ = srv.Run(t.Context())
	}()
	go func() {
		send(t, serverWriter,
			request(1, "initialize", `{}`),
			request(2, "test/progress", ``))
	}()

	client := jsonrpc.NewConn(clientReader, serverWriter)
	var kinds []string
	sawCreate := false
	for len(kinds) < 3 {
		msg, err := client.Read()
		if err != nil {
			t.Fatalf("client read: %v", err)
		}
		if msg.IsRequest() && msg.Method == "window/workDoneProgress/create" {
			if err := client.Reply(msg.ID, nil, nil); err != nil {
				t.Fatalf("client reply: %v", err)
			}
			sawCreate = true
			continue
		}
		if msg.IsNotification() && msg.Method == "$/progress" {
			var params struct {
				Value struct {
					Kind string `json:"kind"`
				} `json:"value"`
			}
			if err := json.Unmarshal(msg.Params, &params); err != nil {
				t.Fatalf("parsing progress: %v", err)
			}
			kinds = append(kinds, params.Value.Kind)
		}
	}
	if !sawCreate {
		t.Error("window/workDoneProgress/create was not sent")
	}
	want := []string{"begin", "report", "end"}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("progress kinds = %v, want %v", kinds, want)
		}
	}

	go func() {
		for {
			if _, err := client.Read(); err != nil {
				return
			}
		}
	}()
	send(t, serverWriter, request(3, "shutdown", ``), notification("exit"))
}

func TestPanicInHandlerReturnsInternalError(t *testing.T) {
	t.Parallel()

	var input bytes.Buffer
	var output bytes.Buffer
	srv := New(jsonrpc.NewConn(&input, &output), "test", CurrentProcess())

	srv.requests["test/panic"] = func(_ context.Context, _ *docstore.View, _ json.RawMessage) (any, error) {
		panic("boom")
	}

	send(t, &input,
		request(1, "initialize", `{}`),
		request(2, "test/panic", ``),
		request(3, "shutdown", ``),
		notification("exit"))

	if err := srv.Run(t.Context()); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v", err)
	}

	client := readerFor(&output)
	msg := responseByID(t, client, "2", 3)
	if msg.Error == nil || msg.Error.Code != jsonrpc.CodeInternalError {
		t.Errorf("error = %+v, want internal error", msg.Error)
	}
}

func TestExitWithUnansweredServerRequest(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	c, _ := startClient(t, `{}`, func(srv *Server) {
		srv.requests["test/askClient"] = func(ctx context.Context, _ *docstore.View, _ json.RawMessage) (any, error) {
			close(started)
			return nil, srv.call(ctx, "client/neverAnswered", nil, nil)
		}
	})
	c.start("test/askClient", "")
	<-started
	c.await("server request", func(m *jsonrpc.Message) bool { return m.IsRequest() && m.Method == "client/neverAnswered" })
	c.shutdownAndExit()
}
