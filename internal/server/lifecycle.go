package server

import (
	"context"
	"encoding/json"

	"github.com/stubbedev/shell-lsp/internal/actions"
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

type CompletionItemCapabilities struct {
	SnippetSupport       bool `json:"snippetSupport"`
	InsertReplaceSupport bool `json:"insertReplaceSupport"`
	LabelDetailsSupport  bool `json:"labelDetailsSupport"`
}

type ClientCapabilities struct {
	General struct {
		PositionEncodings []string `json:"positionEncodings"`
	} `json:"general"`
	TextDocument struct {
		Diagnostic         *struct{} `json:"diagnostic"`
		PublishDiagnostics struct {
			CodeDescriptionSupport bool `json:"codeDescriptionSupport"`
		} `json:"publishDiagnostics"`
		Hover struct {
			ContentFormat []string `json:"contentFormat"`
		} `json:"hover"`
		Completion struct {
			CompletionItem CompletionItemCapabilities `json:"completionItem"`
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
			DocumentChanges         bool      `json:"documentChanges"`
			ResourceOperations      []string  `json:"resourceOperations"`
			ChangeAnnotationSupport *struct{} `json:"changeAnnotationSupport"`
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
	PositionEncoding                string                   `json:"positionEncoding,omitempty"`
	TextDocumentSync                *TextDocumentSyncOptions `json:"textDocumentSync,omitempty"`
	DiagnosticProvider              *DiagnosticOptions       `json:"diagnosticProvider,omitempty"`
	HoverProvider                   bool                     `json:"hoverProvider"`
	CompletionProvider              *CompletionOptions       `json:"completionProvider,omitempty"`
	DefinitionProvider              bool                     `json:"definitionProvider"`
	DeclarationProvider             bool                     `json:"declarationProvider"`
	TypeDefinitionProvider          bool                     `json:"typeDefinitionProvider"`
	ImplementationProvider          bool                     `json:"implementationProvider"`
	DocumentSymbolProvider          bool                     `json:"documentSymbolProvider"`
	FoldingRangeProvider            bool                     `json:"foldingRangeProvider"`
	WorkspaceSymbolProvider         bool                     `json:"workspaceSymbolProvider"`
	DocumentFormattingProvider      bool                     `json:"documentFormattingProvider"`
	DocumentRangeFormattingProvider *RangeFormattingOptions  `json:"documentRangeFormattingProvider,omitempty"`
	CodeActionProvider              *CodeActionOptions       `json:"codeActionProvider,omitempty"`
	ExecuteCommandProvider          *ExecuteCommandOptions   `json:"executeCommandProvider,omitempty"`
	ReferencesProvider              bool                     `json:"referencesProvider"`
	DocumentHighlightProvider       bool                     `json:"documentHighlightProvider"`
	RenameProvider                  *RenameOptions           `json:"renameProvider,omitempty"`
	SemanticTokensProvider          *SemanticTokensOptions   `json:"semanticTokensProvider,omitempty"`
	SelectionRangeProvider          bool                     `json:"selectionRangeProvider"`
	SignatureHelpProvider           *SignatureHelpOptions    `json:"signatureHelpProvider,omitempty"`
	InlayHintProvider               *InlayHintOptions        `json:"inlayHintProvider,omitempty"`
	DocumentLinkProvider            *DocumentLinkOptions     `json:"documentLinkProvider,omitempty"`
	CodeLensProvider                *CodeLensOptions         `json:"codeLensProvider,omitempty"`
	CallHierarchyProvider           bool                     `json:"callHierarchyProvider"`
	InlineValueProvider             bool                     `json:"inlineValueProvider"`
	MonikerProvider                 bool                     `json:"monikerProvider"`
	LinkedEditingRangeProvider      bool                     `json:"linkedEditingRangeProvider"`
	Workspace                       *WorkspaceCapabilities   `json:"workspace,omitempty"`
}

const incrementalSync = 2

var (
	completionTriggers   = []string{"$", "{", "/", "-", ":", "@", "[", "%", "=", ","}
	signatureHelpOptions = SignatureHelpOptions{TriggerCharacters: []string{" "}, RetriggerCharacters: []string{" "}}
)

func ensure[T any](field **T) *T {
	if *field == nil {
		*field = new(T)
	}
	return *field
}

type RenameOptions struct {
	PrepareProvider bool `json:"prepareProvider"`
}

type RangeFormattingOptions struct {
	RangesSupport bool `json:"rangesSupport"`
}

type CodeActionOptions struct {
	CodeActionKinds []actions.Kind `json:"codeActionKinds"`
	ResolveProvider bool           `json:"resolveProvider"`
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

func (s *Server) handleInitialize(_ context.Context, _ *docstore.View, initParams InitializeParams) (any, error) {
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
	advertised := s.capabilities
	advertised.PositionEncoding = enc.String()
	return InitializeResult{
		Capabilities: advertised,
		ServerInfo:   ServerInfo{Name: "shell-lsp", Version: s.version},
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

func (s *Server) handleShutdown(context.Context, *docstore.View, json.RawMessage) (any, error) {
	s.mu.Lock()
	s.shutdown = true
	s.mu.Unlock()
	if s.push != nil {
		s.push.close()
	}
	return nil, nil
}

func (s *Server) handleCancelRequest(_ context.Context, cancelParams CancelParams) error {
	if found, ok := s.cancels.LoadAndDelete(string(cancelParams.ID)); ok {
		if cancel, ok := found.(context.CancelFunc); ok {
			cancel()
		}
	}
	return nil
}

func (s *Server) handleSetTrace(_ context.Context, traceParams SetTraceParams) error {
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
