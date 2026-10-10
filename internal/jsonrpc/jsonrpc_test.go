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
