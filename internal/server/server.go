package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
)

const exitMethod = "exit"

type InitializeParams struct {
	ProcessID          *int            `json:"processId"`
	RootPath           string          `json:"rootPath"`
	RootURI            string          `json:"rootUri"`
	ClientCapabilities json.RawMessage `json:"capabilities"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type TextDocumentSyncOptions struct {
	OpenClose bool `json:"openClose"`
	Change    int  `json:"change"`
}

type ServerCapabilities struct {
	TextDocumentSync TextDocumentSyncOptions `json:"textDocumentSync"`
}

type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	ServerInfo   ServerInfo         `json:"serverInfo"`
}

type Server struct {
	conn    *jsonrpc.Conn
	version string

	mu       sync.Mutex
	shutdown bool
}

func New(conn *jsonrpc.Conn, version string) *Server {
	return &Server{conn: conn, version: version}
}

func (s *Server) Run(ctx context.Context) error {
	for {
		msg, err := s.conn.Read()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				s.mu.Lock()
				exited := s.shutdown
				s.mu.Unlock()
				if exited {
					return nil
				}
				return errors.New("client closed input without exit")
			}
			return fmt.Errorf("reading message: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		switch {
		case msg.IsResponse():
		case msg.IsNotification():
			if msg.Method == exitMethod {
				s.mu.Lock()
				done := s.shutdown
				s.mu.Unlock()
				if done {
					return nil
				}
				return errors.New("exit before shutdown")
			}
		case msg.IsRequest():
			if err := s.handleRequest(msg); err != nil {
				return err
			}
		}
	}
}

func (s *Server) handleRequest(msg *jsonrpc.Message) error {
	switch msg.Method {
	case "initialize":
		var params InitializeParams
		if len(msg.Params) > 0 {
			if err := json.Unmarshal(msg.Params, &params); err != nil {
				return s.conn.Reply(msg.ID, nil, jsonrpc.NewError(
					jsonrpc.CodeInvalidParams, "parsing initialize params: %v", err))
			}
		}
		result := InitializeResult{
			Capabilities: ServerCapabilities{
				TextDocumentSync: TextDocumentSyncOptions{OpenClose: true, Change: 1},
			},
			ServerInfo: ServerInfo{Name: "shell-lsp", Version: s.version},
		}
		return s.conn.Reply(msg.ID, result, nil)
	case "shutdown":
		s.mu.Lock()
		s.shutdown = true
		s.mu.Unlock()
		return s.conn.Reply(msg.ID, nil, nil)
	default:
		return s.conn.Reply(msg.ID, nil, jsonrpc.NewError(
			jsonrpc.CodeMethodNotFound, "method %q not found", msg.Method))
	}
}
