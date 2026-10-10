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
	ProcessID             *int                  `json:"processId"`
	RootPath              string                `json:"rootPath"`
	RootURI               string                `json:"rootUri"`
	Capabilities          ClientCapabilities    `json:"capabilities"`
	InitializationOptions json.RawMessage       `json:"initializationOptions"`
	WorkspaceFolders      []lsp.WorkspaceFolder `json:"workspaceFolders"`
	Trace                 TraceValue            `json:"trace"`
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
		Completion struct {
			CompletionItem struct {
				SnippetSupport bool `json:"snippetSupport"`
			} `json:"completionItem"`
		} `json:"completion"`
		DocumentSymbol struct {
			HierarchicalDocumentSymbolSupport bool `json:"hierarchicalDocumentSymbolSupport"`
		} `json:"documentSymbol"`
		FoldingRange struct {
			RangeLimit int `json:"rangeLimit"`
		} `json:"foldingRange"`
		CodeAction struct {
			CodeActionLiteralSupport *struct{} `json:"codeActionLiteralSupport"`
			ResolveSupport           struct {
				Properties []string `json:"properties"`
			} `json:"resolveSupport"`
		} `json:"codeAction"`
	} `json:"textDocument"`
	Window struct {
		WorkDoneProgress bool `json:"workDoneProgress"`
	} `json:"window"`
	Workspace struct {
		Diagnostics struct {
			RefreshSupport bool `json:"refreshSupport"`
		} `json:"diagnostics"`
		WorkspaceEdit struct {
			DocumentChanges bool `json:"documentChanges"`
		} `json:"workspaceEdit"`
		Configuration  bool `json:"configuration"`
		SemanticTokens struct {
			RefreshSupport bool `json:"refreshSupport"`
		} `json:"semanticTokens"`
		InlayHint struct {
			RefreshSupport bool `json:"refreshSupport"`
		} `json:"inlayHint"`
		CodeLens struct {
			RefreshSupport bool `json:"refreshSupport"`
		} `json:"codeLens"`
		DidChangeWatchedFiles struct {
			DynamicRegistration bool `json:"dynamicRegistration"`
		} `json:"didChangeWatchedFiles"`
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
	PositionEncoding                string                  `json:"positionEncoding,omitempty"`
	TextDocumentSync                TextDocumentSyncOptions `json:"textDocumentSync"`
	DiagnosticProvider              DiagnosticOptions       `json:"diagnosticProvider"`
	HoverProvider                   bool                    `json:"hoverProvider"`
	CompletionProvider              CompletionOptions       `json:"completionProvider"`
	DefinitionProvider              bool                    `json:"definitionProvider"`
	DeclarationProvider             bool                    `json:"declarationProvider"`
	TypeDefinitionProvider          bool                    `json:"typeDefinitionProvider"`
	ImplementationProvider          bool                    `json:"implementationProvider"`
	DocumentSymbolProvider          bool                    `json:"documentSymbolProvider"`
	FoldingRangeProvider            bool                    `json:"foldingRangeProvider"`
	WorkspaceSymbolProvider         bool                    `json:"workspaceSymbolProvider"`
	DocumentFormattingProvider      bool                    `json:"documentFormattingProvider"`
	DocumentRangeFormattingProvider RangeFormattingOptions  `json:"documentRangeFormattingProvider"`
	CodeActionProvider              CodeActionOptions       `json:"codeActionProvider"`
	ExecuteCommandProvider          ExecuteCommandOptions   `json:"executeCommandProvider"`
	ReferencesProvider              bool                    `json:"referencesProvider"`
	DocumentHighlightProvider       bool                    `json:"documentHighlightProvider"`
	RenameProvider                  RenameOptions           `json:"renameProvider"`
	SemanticTokensProvider          SemanticTokensOptions   `json:"semanticTokensProvider"`
	SelectionRangeProvider          bool                    `json:"selectionRangeProvider"`
	SignatureHelpProvider           SignatureHelpOptions    `json:"signatureHelpProvider"`
	InlayHintProvider               InlayHintOptions        `json:"inlayHintProvider"`
	DocumentLinkProvider            DocumentLinkOptions     `json:"documentLinkProvider"`
	CodeLensProvider                CodeLensOptions         `json:"codeLensProvider"`
	CallHierarchyProvider           bool                    `json:"callHierarchyProvider"`
	InlineValueProvider             bool                    `json:"inlineValueProvider"`
	MonikerProvider                 bool                    `json:"monikerProvider"`
	LinkedEditingRangeProvider      bool                    `json:"linkedEditingRangeProvider"`
	Workspace                       WorkspaceCapabilities   `json:"workspace"`
}

type RenameOptions struct {
	PrepareProvider bool `json:"prepareProvider"`
}

type RangeFormattingOptions struct {
	RangesSupport bool `json:"rangesSupport"`
}

type CodeActionOptions struct {
	CodeActionKinds []string `json:"codeActionKinds"`
	ResolveProvider bool     `json:"resolveProvider"`
}

type ExecuteCommandOptions struct {
	Commands []string `json:"commands"`
}

type CompletionOptions struct {
	TriggerCharacters []string `json:"triggerCharacters"`
	ResolveProvider   bool     `json:"resolveProvider"`
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
	s.workspace.SetRoots(workspaceRoots(initParams))
	s.mu.Lock()
	s.settings = settings
	s.client = caps
	s.initialized = true
	s.mu.Unlock()
	return InitializeResult{
		Capabilities: ServerCapabilities{
			PositionEncoding:                enc.String(),
			TextDocumentSync:                TextDocumentSyncOptions{OpenClose: true, Change: 2},
			DiagnosticProvider:              DiagnosticOptions{},
			HoverProvider:                   true,
			CompletionProvider:              CompletionOptions{TriggerCharacters: []string{"$", "{", "/", "-"}, ResolveProvider: true},
			DefinitionProvider:              true,
			DeclarationProvider:             true,
			TypeDefinitionProvider:          true,
			ImplementationProvider:          true,
			DocumentSymbolProvider:          true,
			FoldingRangeProvider:            true,
			WorkspaceSymbolProvider:         true,
			DocumentFormattingProvider:      true,
			DocumentRangeFormattingProvider: RangeFormattingOptions{RangesSupport: true},
			CodeActionProvider:              CodeActionOptions{CodeActionKinds: codeActionKinds, ResolveProvider: true},
			ExecuteCommandProvider:          ExecuteCommandOptions{Commands: serverCommands},
			ReferencesProvider:              true,
			DocumentHighlightProvider:       true,
			RenameProvider:                  RenameOptions{PrepareProvider: true},
			SemanticTokensProvider:          semanticTokensOptions,
			SelectionRangeProvider:          true,
			SignatureHelpProvider:           SignatureHelpOptions{TriggerCharacters: []string{" "}, RetriggerCharacters: []string{" "}},
			InlayHintProvider:               InlayHintOptions{ResolveProvider: true},
			DocumentLinkProvider:            DocumentLinkOptions{},
			CodeLensProvider:                CodeLensOptions{ResolveProvider: true},
			CallHierarchyProvider:           true,
			InlineValueProvider:             true,
			MonikerProvider:                 true,
			LinkedEditingRangeProvider:      true,
			Workspace:                       workspaceCapabilities,
		},
		ServerInfo: ServerInfo{Name: "shell-lsp", Version: s.version},
	}, nil
}

func (s *Server) clientCapabilities() ClientCapabilities {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client
}

func (s *Server) handleInitialized(ctx context.Context, _ json.RawMessage) error {
	s.warmUp(ctx)
	go func() {
		s.loadConfiguration(ctx)
		s.registerWatchers(ctx)
		s.indexInBackground(ctx)
	}()
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
