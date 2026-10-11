package jsonrpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestWriteReadRoundTrip(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	conn := NewConn(&buf, &buf)
	msg := &Message{
		JSONRPC: Version,
		ID:      json.RawMessage("1"),
		Method:  "initialize",
		Params:  json.RawMessage(`{"processId":42}`),
	}
	if err := conn.Write(msg); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	got, err := conn.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if !got.IsRequest() {
		t.Errorf("message classified as request = false, got %+v", got)
	}
	if got.Method != "initialize" {
		t.Errorf("Method = %q, want initialize", got.Method)
	}
	if string(got.Params) != `{"processId":42}` {
		t.Errorf("Params = %s, want original JSON", got.Params)
	}
}

func TestWriteUsesCRLFHeader(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	conn := NewConn(nil, &buf)
	if err := conn.Write(&Message{JSONRPC: Version, ID: json.RawMessage("7")}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	raw := buf.String()
	if !strings.HasPrefix(raw, "Content-Length: ") || !strings.Contains(raw, "\r\n\r\n") {
		t.Errorf("frame header malformed: %q", raw)
	}
}

func TestClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                        string
		msg                         Message
		request, notification, resp bool
	}{
		{name: "request", msg: Message{ID: json.RawMessage("1"), Method: "m"}, request: true},
		{name: "notification", msg: Message{Method: "m"}, notification: true},
		{name: "response", msg: Message{ID: json.RawMessage("1"), Result: json.RawMessage("null")}, resp: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.msg.IsRequest(); got != tc.request {
				t.Errorf("IsRequest() = %v, want %v", got, tc.request)
			}
			if got := tc.msg.IsNotification(); got != tc.notification {
				t.Errorf("IsNotification() = %v, want %v", got, tc.notification)
			}
			if got := tc.msg.IsResponse(); got != tc.resp {
				t.Errorf("IsResponse() = %v, want %v", got, tc.resp)
			}
		})
	}
}

func TestReplyNullResult(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	conn := NewConn(nil, &buf)
	if err := conn.Reply(json.RawMessage("2"), nil, nil); err != nil {
		t.Fatalf("Reply() error = %v", err)
	}
	body := buf.String()
	if !strings.HasSuffix(body, `{"jsonrpc":"2.0","id":2,"result":null}`) {
		t.Errorf("reply body = %q, want explicit null result", body)
	}
}

func TestReplyError(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	conn := NewConn(&buf, &buf)
	rpcErr := NewError(CodeMethodNotFound, "method %s not found", "nope")
	if err := conn.Reply(json.RawMessage(`"abc"`), nil, rpcErr); err != nil {
		t.Fatalf("Reply() error = %v", err)
	}
	got, err := conn.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got.Error == nil || got.Error.Code != CodeMethodNotFound {
		t.Errorf("Error = %+v, want code %d", got.Error, CodeMethodNotFound)
	}
	if got.Error.Error() == "" {
		t.Error("Error.Error() is empty")
	}
}

func TestReadRejectsMissingHeader(t *testing.T) {
	t.Parallel()

	conn := NewConn(strings.NewReader("\r\n{}"), nil)
	if _, err := conn.Read(); err == nil {
		t.Error("Read() with no Content-Length succeeded, want error")
	}
}

func TestReadRejectsGarbage(t *testing.T) {
	t.Parallel()

	conn := NewConn(strings.NewReader("Content-Length: 3\r\n\r\n{}}"), nil)
	if _, err := conn.Read(); err == nil {
		t.Error("Read() with invalid JSON succeeded, want error")
	}
}

func TestReadEOF(t *testing.T) {
	t.Parallel()

	conn := NewConn(strings.NewReader(""), nil)
	if _, err := conn.Read(); !errors.Is(err, io.EOF) {
		t.Errorf("Read() error = %v, want io.EOF", err)
	}
}

type countingWriter struct {
	bytes.Buffer
	writes int
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.writes++
	return w.Buffer.Write(p)
}

func TestEachFrameIsOneWrite(t *testing.T) {
	t.Parallel()

	var w countingWriter
	conn := NewConn(nil, &w)
	if err := conn.Reply(json.RawMessage("3"), map[string][]int{"data": {1, 2}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := conn.Notify("window/logMessage", map[string]string{"message": "<a&b>"}); err != nil {
		t.Fatal(err)
	}
	if err := conn.Request(json.RawMessage("4"), "workspace/configuration", nil); err != nil {
		t.Fatal(err)
	}
	want := "Content-Length: 48\r\n\r\n" + `{"jsonrpc":"2.0","id":3,"result":{"data":[1,2]}}` +
		"Content-Length: 90\r\n\r\n" + `{"jsonrpc":"2.0","method":"window/logMessage","params":{"message":"\u003ca\u0026b\u003e"}}` +
		"Content-Length: 59\r\n\r\n" + `{"jsonrpc":"2.0","id":4,"method":"workspace/configuration"}`
	if w.String() != want {
		t.Errorf("frames =\n%q\nwant\n%q", w.String(), want)
	}
	if w.writes != 3 {
		t.Errorf("writes = %d, want one per frame", w.writes)
	}
}

func TestReadMessagesOutliveTheNextRead(t *testing.T) {
	t.Parallel()

	conn := NewConn(strings.NewReader(
		"Content-Length: 50\r\n\r\n"+`{"jsonrpc":"2.0","method":"a","params":{"n":1111}}`+
			"Content-Length: 50\r\n\r\n"+`{"jsonrpc":"2.0","method":"b","params":{"n":2222}}`), nil)
	first, err := conn.Read()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(); err != nil {
		t.Fatal(err)
	}
	if first.Method != "a" || string(first.Params) != `{"n":1111}` {
		t.Errorf("first message changed after the next read: %s %s", first.Method, first.Params)
	}
}

func TestReplyReportsUnencodableResults(t *testing.T) {
	t.Parallel()

	var w countingWriter
	conn := NewConn(nil, &w)
	err := conn.Reply(json.RawMessage("5"), map[string]any{"f": func() {}}, nil)
	if err == nil || !strings.Contains(err.Error(), "result of response 5") {
		t.Errorf("Reply error = %v, want a marshaling error naming the response", err)
	}
	if w.writes != 0 {
		t.Errorf("writes = %d, want nothing written for a failed frame", w.writes)
	}
	if err := conn.Reply(json.RawMessage("6"), nil, nil); err != nil || !strings.HasSuffix(w.String(), `{"jsonrpc":"2.0","id":6,"result":null}`) {
		t.Errorf("frame after a failed one = %q, %v", w.String(), err)
	}
}

func TestLargeMessagesDoNotKeepTheirBuffers(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	conn := NewConn(&buf, &buf)
	large := json.RawMessage(`"` + strings.Repeat("x", 4*retainedBufferSize) + `"`)
	if err := conn.Notify("big", large); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(); err != nil {
		t.Fatal(err)
	}
	if conn.frame.Cap() > retainedBufferSize || cap(conn.body) > retainedBufferSize {
		t.Errorf("buffers kept after a %d byte message: frame %d, body %d", len(large), conn.frame.Cap(), cap(conn.body))
	}
	if err := conn.Notify("small", json.RawMessage(`1`)); err != nil {
		t.Fatal(err)
	}
	if msg, err := conn.Read(); err != nil || msg.Method != "small" {
		t.Errorf("read after a large message = %+v, %v", msg, err)
	}
}
