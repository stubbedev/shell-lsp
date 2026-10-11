package jsonrpc

import (
	"bufio"
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"sync"
)

const Version = "2.0"

const (
	CodeParseError           = -32700
	CodeInvalidRequest       = -32600
	CodeMethodNotFound       = -32601
	CodeInvalidParams        = -32602
	CodeInternalError        = -32603
	CodeServerClosing        = -32000
	CodeRequestFailed        = -32803
	CodeContentModified      = -32801
	CodeServerNotInitialized = -32002
	CodeRequestCancelled     = -32800
)

const maxBodySize = 64 << 20

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

const (
	readBufferSize     = 64 << 10
	retainedBufferSize = 1 << 20
	lengthName         = "Content-Length"
	lengthHeader       = lengthName + ": "
	headerEnd          = "\r\n\r\n"
	headerReserve      = len(lengthHeader) + 20 + len(headerEnd)
)

type Conn struct {
	reader *bufio.Reader
	body   []byte
	writer io.Writer
	mu     sync.Mutex
	frame  bytes.Buffer
}

func NewConn(r io.Reader, w io.Writer) *Conn {
	return &Conn{reader: bufio.NewReaderSize(r, readBufferSize), writer: w}
}

func (c *Conn) Read() (*Message, error) {
	contentLength := -1
	for {
		line, err := c.reader.ReadSlice('\n')
		if err != nil {
			if errors.Is(err, bufio.ErrBufferFull) {
				return nil, fmt.Errorf("header line longer than %d bytes", readBufferSize)
			}
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
		if bytes.EqualFold(name, []byte(lengthName)) {
			n, err := strconv.Atoi(string(bytes.TrimSpace(value)))
			if err != nil || n < 0 {
				return nil, fmt.Errorf("invalid Content-Length %q", value)
			}
			if n > maxBodySize {
				return nil, fmt.Errorf("Content-Length %d exceeds limit %d", n, maxBodySize)
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return nil, errors.New("missing Content-Length header")
	}
	defer c.releaseLargeBody()
	c.body = slices.Grow(c.body[:0], contentLength)[:contentLength]
	if _, err := io.ReadFull(c.reader, c.body); err != nil {
		return nil, fmt.Errorf("reading %d byte body: %w", contentLength, err)
	}
	var msg Message
	if err := json.Unmarshal(c.body, &msg); err != nil {
		return nil, fmt.Errorf("parsing body %q: %w", c.body, err)
	}
	return &msg, nil
}

func (c *Conn) Write(msg *Message) error {
	return c.send(func() error {
		if err := c.encode(msg); err != nil {
			return &encodeError{what: "message", err: err}
		}
		return nil
	})
}

func (c *Conn) Reply(id json.RawMessage, result any, rpcErr *Error) error {
	if rpcErr != nil {
		return c.envelope(id, "", "error", rpcErr)
	}
	err := c.envelope(id, "", "result", result)
	if unencodable, ok := errors.AsType[*encodeError](err); ok {
		return errors.Join(err, c.envelope(id, "", "error", NewError(CodeInternalError, "%v", unencodable)))
	}
	return err
}

type encodeError struct {
	what string
	err  error
}

func (e *encodeError) Error() string { return "marshaling " + e.what + ": " + e.err.Error() }

func (e *encodeError) Unwrap() error { return e.err }

func (c *Conn) Request(id json.RawMessage, method string, params any) error {
	return c.envelope(id, method, "params", params)
}

func (c *Conn) Notify(method string, params any) error {
	return c.envelope(nil, method, "params", params)
}

func (c *Conn) envelope(id json.RawMessage, method, member string, value any) error {
	return c.send(func() error {
		c.frame.WriteString(`{"jsonrpc":"` + Version + `"`)
		if id != nil {
			c.frame.WriteString(`,"id":`)
			c.frame.Write(id)
		}
		if method != "" {
			c.frame.WriteString(`,"method":`)
			if err := c.encode(method); err != nil {
				return &encodeError{what: strconv.Quote(method), err: err}
			}
		}
		if value != nil || member == "result" {
			c.frame.WriteString(`,"` + member + `":`)
			if err := c.encode(value); err != nil {
				return &encodeError{what: member + " of " + describe(id, method), err: err}
			}
		}
		c.frame.WriteByte('}')
		return nil
	})
}

func describe(id json.RawMessage, method string) string {
	if method != "" {
		return method
	}
	return "response " + string(id)
}

func (c *Conn) encode(v any) error {
	return jsonv2.MarshalWrite(&c.frame, v, json.DefaultOptionsV1())
}

func (c *Conn) releaseLargeBody() {
	if cap(c.body) > retainedBufferSize {
		c.body = nil
	}
}

func (c *Conn) releaseLargeFrame() {
	if c.frame.Cap() > retainedBufferSize {
		c.frame = bytes.Buffer{}
	}
}

func (c *Conn) send(body func() error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	defer c.releaseLargeFrame()
	c.frame.Reset()
	var reserve [headerReserve]byte
	c.frame.Write(reserve[:])
	if err := body(); err != nil {
		return err
	}
	frame := c.frame.Bytes()
	h := append(strconv.AppendInt(append(reserve[:0], lengthHeader...), int64(len(frame)-headerReserve), 10), headerEnd...)
	start := headerReserve - len(h)
	copy(frame[start:], h)
	if _, err := c.writer.Write(frame[start:]); err != nil {
		return fmt.Errorf("writing %d byte frame: %w", len(frame)-start, err)
	}
	return nil
}
