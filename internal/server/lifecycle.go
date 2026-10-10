package server

import (
	"context"
	"encoding/json"

	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
)

type InitializeParams struct {
	ProcessID          *int            `json:"processId"`
	RootPath           string          `json:"rootPath"`
	RootURI            string          `json:"rootUri"`
	ClientCapabilities json.RawMessage `json:"capabilities"`
	Trace              TraceValue      `json:"trace"`
}

type CancelParams struct {
	ID json.RawMessage `json:"id"`
}

type SetTraceParams struct {
	Value TraceValue `json:"value"`
}

type LogMessageParams struct {
	Type    int    `json:"type"`
	Message string `json:"message"`
}

type LogTraceParams struct {
	Message string `json:"message"`
	Verbose string `json:"verbose,omitempty"`
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

func (s *Server) handleInitialize(_ context.Context, params json.RawMessage) (any, error) {
	var initParams InitializeParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &initParams); err != nil {
			return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing initialize params: %v", err)
		}
	}
	if initParams.Trace != "" {
		s.setTrace(initParams.Trace)
	}
	s.mu.Lock()
	s.initialized = true
	s.mu.Unlock()
	return InitializeResult{
		Capabilities: ServerCapabilities{
			TextDocumentSync: TextDocumentSyncOptions{OpenClose: true, Change: 1},
		},
		ServerInfo: ServerInfo{Name: "shell-lsp", Version: s.version},
	}, nil
}

func (s *Server) handleShutdown(_ context.Context, _ json.RawMessage) (any, error) {
	s.mu.Lock()
	s.shutdown = true
	s.mu.Unlock()
	return nil, nil
}

func (s *Server) handleCancelRequest(_ context.Context, params json.RawMessage) error {
	var cancelParams CancelParams
	if err := json.Unmarshal(params, &cancelParams); err != nil {
		return err
	}
	if found, ok := s.cancels.LoadAndDelete(string(cancelParams.ID)); ok {
		if cancel, ok := found.(context.CancelFunc); ok {
			cancel()
		}
	}
	return nil
}

func (s *Server) handleSetTrace(_ context.Context, params json.RawMessage) error {
	var traceParams SetTraceParams
	if err := json.Unmarshal(params, &traceParams); err != nil {
		return err
	}
	s.setTrace(traceParams.Value)
	return nil
}

func (s *Server) setTrace(value TraceValue) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch value {
	case TraceOff, TraceMessages, TraceVerbose:
		s.trace = value
	default:
		s.logger.Warn("ignoring unknown trace value", "value", string(value))
	}
}

func (s *Server) handleClientLogMessage(_ context.Context, params json.RawMessage) error {
	var logParams LogMessageParams
	if err := json.Unmarshal(params, &logParams); err != nil {
		return err
	}
	s.logger.Info("client message", "type", logParams.Type, "message", logParams.Message)
	return nil
}
