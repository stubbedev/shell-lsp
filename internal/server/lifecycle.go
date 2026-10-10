package server

import (
	"context"
	"encoding/json"

	"github.com/stubbedev/shell-lsp/internal/config"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

type InitializeParams struct {
	ProcessID             *int               `json:"processId"`
	RootPath              string             `json:"rootPath"`
	RootURI               string             `json:"rootUri"`
	Capabilities          ClientCapabilities `json:"capabilities"`
	InitializationOptions json.RawMessage    `json:"initializationOptions"`
	Trace                 TraceValue         `json:"trace"`
}

type ClientCapabilities struct {
	General struct {
		PositionEncodings []string `json:"positionEncodings"`
	} `json:"general"`
	TextDocument struct {
		Diagnostic *struct{} `json:"diagnostic"`
		Hover      struct {
			ContentFormat []string `json:"contentFormat"`
		} `json:"hover"`
	} `json:"textDocument"`
	Workspace struct {
		Diagnostics struct {
			RefreshSupport bool `json:"refreshSupport"`
		} `json:"diagnostics"`
	} `json:"workspace"`
}

type CancelParams struct {
	ID json.RawMessage `json:"id"`
}

type SetTraceParams struct {
	Value TraceValue `json:"value"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type TextDocumentSyncOptions struct {
	OpenClose bool `json:"openClose"`
	Change    int  `json:"change"`
}

type DiagnosticOptions struct {
	InterFileDependencies bool `json:"interFileDependencies"`
	WorkspaceDiagnostics  bool `json:"workspaceDiagnostics"`
}

type ServerCapabilities struct {
	PositionEncoding   string                  `json:"positionEncoding,omitempty"`
	TextDocumentSync   TextDocumentSyncOptions `json:"textDocumentSync"`
	DiagnosticProvider DiagnosticOptions       `json:"diagnosticProvider"`
	HoverProvider      bool                    `json:"hoverProvider"`
}

type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	ServerInfo   ServerInfo         `json:"serverInfo"`
}

func (s *Server) handleInitialize(_ context.Context, _ *docstore.View, params json.RawMessage) (any, error) {
	initParams, err := lsp.DecodeParams[InitializeParams](params)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing initialize params: %v", err)
	}
	settings, err := config.Parse(initParams.InitializationOptions)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "initializationOptions: %v", err)
	}
	if initParams.Trace != "" {
		s.setTrace(initParams.Trace)
	}
	caps := initParams.Capabilities
	enc := negotiateEncoding(caps.General.PositionEncodings)
	s.docs.SetEncoding(enc)
	if caps.TextDocument.Diagnostic == nil {
		s.push = newPushScheduler(diagnosticsDebounce, s.publishDiagnostics)
	}
	s.mu.Lock()
	s.settings = settings
	s.refreshSupport = caps.Workspace.Diagnostics.RefreshSupport
	s.hoverFormats = caps.TextDocument.Hover.ContentFormat
	s.initialized = true
	s.mu.Unlock()
	return InitializeResult{
		Capabilities: ServerCapabilities{
			PositionEncoding:   enc.String(),
			TextDocumentSync:   TextDocumentSyncOptions{OpenClose: true, Change: 2},
			DiagnosticProvider: DiagnosticOptions{},
			HoverProvider:      true,
		},
		ServerInfo: ServerInfo{Name: "shell-lsp", Version: s.version},
	}, nil
}

func (s *Server) handleInitialized(_ context.Context, _ json.RawMessage) error {
	return nil
}

func (s *Server) handleShutdown(_ context.Context, _ *docstore.View, _ json.RawMessage) (any, error) {
	s.mu.Lock()
	s.shutdown = true
	s.mu.Unlock()
	if s.push != nil {
		s.push.close()
	}
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
	var logParams lsp.LogMessageParams
	if err := json.Unmarshal(params, &logParams); err != nil {
		return err
	}
	s.logger.Info("client message", "type", logParams.Type, "message", logParams.Message)
	return nil
}
