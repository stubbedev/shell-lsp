package server

import (
	"encoding/json"
	"io"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
)

const clientTimeout = 10 * time.Second

type testClient struct {
	t      *testing.T
	conn   *jsonrpc.Conn
	writer *io.PipeWriter
	runErr chan error

	mu       sync.Mutex
	nextID   int
	messages []*jsonrpc.Message
	arrived  chan struct{}
}

func startClient(t *testing.T, initializeParams string, setup ...func(*Server)) (*testClient, *Server) {
	t.Helper()

	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()
	srv := New(jsonrpc.NewConn(serverReader, serverWriter), "test")
	for _, fn := range setup {
		fn(srv)
	}
	c := &testClient{
		t:       t,
		conn:    jsonrpc.NewConn(clientReader, clientWriter),
		writer:  clientWriter,
		runErr:  make(chan error, 1),
		arrived: make(chan struct{}, 1),
	}
	go func() {
		c.runErr <- srv.Run(t.Context())
		_ = serverWriter.Close()
	}()
	go c.readLoop()
	c.call("initialize", initializeParams)
	t.Cleanup(c.close)
	return c, srv
}

func (c *testClient) readLoop() {
	for {
		msg, err := c.conn.Read()
		if err != nil {
			return
		}
		c.mu.Lock()
		c.messages = append(c.messages, msg)
		c.mu.Unlock()
		select {
		case c.arrived <- struct{}{}:
		default:
		}
	}
}

func (c *testClient) send(msg *jsonrpc.Message) {
	c.t.Helper()

	if err := c.conn.Write(msg); err != nil {
		c.t.Fatalf("writing %s: %v", msg.Method, err)
	}
}

func (c *testClient) notify(method, params string) {
	c.t.Helper()

	msg := notification(method)
	msg.Params = json.RawMessage(params)
	c.send(msg)
}

func (c *testClient) call(method, params string) *jsonrpc.Message {
	c.t.Helper()

	return c.awaitResponse(c.start(method, params), method)
}

func (c *testClient) start(method, params string) string {
	c.t.Helper()

	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.mu.Unlock()
	c.send(request(id, method, params))
	return strconv.Itoa(id)
}

func (c *testClient) awaitResponse(id, method string) *jsonrpc.Message {
	c.t.Helper()

	return c.await("response to "+method, func(m *jsonrpc.Message) bool {
		return m.IsResponse() && string(m.ID) == id
	})
}

func (c *testClient) await(what string, match func(*jsonrpc.Message) bool) *jsonrpc.Message {
	c.t.Helper()

	deadline := time.After(clientTimeout)
	for {
		c.mu.Lock()
		for i, m := range c.messages {
			if match(m) {
				c.messages = append(c.messages[:i], c.messages[i+1:]...)
				c.mu.Unlock()
				return m
			}
		}
		c.mu.Unlock()
		select {
		case <-c.arrived:
		case <-time.After(20 * time.Millisecond):
		case <-deadline:
			c.t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func (c *testClient) awaitNotification(method string, match func(params json.RawMessage) bool) json.RawMessage {
	c.t.Helper()

	return c.await("notification "+method, func(m *jsonrpc.Message) bool {
		return m.IsNotification() && m.Method == method && match(m.Params)
	}).Params
}

func (c *testClient) answerServerRequest(method string) json.RawMessage {
	c.t.Helper()

	msg := c.await("server request "+method, func(m *jsonrpc.Message) bool {
		return m.IsRequest() && m.Method == method
	})
	if err := c.conn.Reply(msg.ID, nil, nil); err != nil {
		c.t.Fatalf("answering %s: %v", method, err)
	}
	return msg.Params
}

func (c *testClient) close() {
	c.call("shutdown", "")
	c.notify("exit", "")
	select {
	case err := <-c.runErr:
		if err != nil {
			c.t.Errorf("Run() error = %v", err)
		}
	case <-time.After(clientTimeout):
		c.t.Error("server did not exit")
	}
	_ = c.writer.Close()
}
