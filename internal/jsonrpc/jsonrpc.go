package jsonrpc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

const Version = "2.0"

const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
	CodeServerClosing  = -32000
)

type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

func (m *Message) IsRequest() bool      { return len(m.ID) > 0 && m.Method != "" }
func (m *Message) IsNotification() bool { return len(m.ID) == 0 && m.Method != "" }
func (m *Message) IsResponse() bool     { return len(m.ID) > 0 && m.Method == "" }

type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

func NewError(code int, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

type Conn struct {
	reader *bufio.Reader
	writer io.Writer
	mu     sync.Mutex
}

func NewConn(r io.Reader, w io.Writer) *Conn {
	return &Conn{reader: bufio.NewReader(r), writer: w}
}

func (c *Conn) Read() (*Message, error) {
	contentLength := -1
	for {
		line, err := c.reader.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			break
		}
		name, value, ok := bytes.Cut(line, []byte{':'})
		if !ok {
			return nil, fmt.Errorf("malformed header %q", line)
		}
		if strings.EqualFold(string(name), "Content-Length") {
			n, err := strconv.Atoi(string(bytes.TrimSpace(value)))
			if err != nil || n < 0 {
				return nil, fmt.Errorf("invalid Content-Length %q", value)
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return nil, errors.New("missing Content-Length header")
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(c.reader, body); err != nil {
		return nil, fmt.Errorf("reading %d byte body: %w", contentLength, err)
	}
	var msg Message
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, fmt.Errorf("parsing body %q: %w", body, err)
	}
	return &msg, nil
}

func (c *Conn) Write(msg *Message) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := fmt.Fprintf(c.writer, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return fmt.Errorf("writing header: %w", err)
	}
	if _, err := c.writer.Write(body); err != nil {
		return fmt.Errorf("writing body: %w", err)
	}
	return nil
}

func (c *Conn) Reply(id json.RawMessage, result any, rpcErr *Error) error {
	msg := &Message{JSONRPC: Version, ID: id}
	if rpcErr != nil {
		msg.Error = rpcErr
	} else if result != nil {
		encoded, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("marshaling result: %w", err)
		}
		msg.Result = encoded
	} else {
		msg.Result = json.RawMessage("null")
	}
	return c.Write(msg)
}

func (c *Conn) Notify(method string, params any) error {
	msg := &Message{JSONRPC: Version, Method: method}
	if params != nil {
		encoded, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("marshaling params: %w", err)
		}
		msg.Params = encoded
	}
	return c.Write(msg)
}
