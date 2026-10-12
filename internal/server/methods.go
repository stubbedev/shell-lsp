package server

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/actions"
	"github.com/stubbedev/shell-lsp/internal/navigation"
)

const (
	configurationMethod         = "workspace/configuration"
	applyEditMethod             = "workspace/applyEdit"
	registerCapabilityMethod    = "client/registerCapability"
	progressCreateMethod        = "window/workDoneProgress/create"
	codeLensRefreshMethod       = "workspace/codeLens/refresh"
	semanticTokensRefreshMethod = "workspace/semanticTokens/refresh"
	inlayHintRefreshMethod      = "workspace/inlayHint/refresh"
	diagnosticRefreshMethod     = "workspace/diagnostic/refresh"
	showMessageMethod           = "window/showMessage"
	logMessageMethod            = "window/logMessage"
	publishDiagnosticsMethod    = "textDocument/publishDiagnostics"
	progressMethod              = "$/progress"
	logTraceMethod              = "$/logTrace"
	didChangeWatchedFilesMethod = "workspace/didChangeWatchedFiles"
)

type direction uint8

const (
	clientRequest direction = iota
	clientNotification
	serverRequest
	serverNotification
)

var directionNames = map[direction]string{
	clientRequest:      "client → server request",
	clientNotification: "client → server notification",
	serverRequest:      "server → client request",
	serverNotification: "server → client notification",
}

type support uint8

const (
	supported support = iota
	notAdvertised
	sent
	notSent
)

var supportNames = map[support]string{
	supported:     "implemented",
	notAdvertised: "not advertised",
	sent:          "sent",
	notSent:       "never sent",
}

type capability struct {
	path      string
	advertise func(*ServerCapabilities)
}

type handler interface {
	install(s *Server, name string)
}

type method struct {
	name       string
	direction  direction
	support    support
	capability capability
	note       string
	handler    handler
}

var methods = []method{
	{"initialize", clientRequest, supported, capability{}, "negotiates position encoding, pull or push diagnostics, workspace folders and settings", onRequest((*Server).handleInitialize)},
	{"shutdown", clientRequest, supported, capability{}, "later requests fail with InvalidRequest", onRequest((*Server).handleShutdown)},
	{"textDocument/willSaveWaitUntil", clientRequest, notAdvertised, capability{path: "textDocumentSync.willSaveWaitUntil"}, "formatting is offered through textDocument/formatting instead", nil},
	{"textDocument/declaration", clientRequest, supported, capability{"declarationProvider", func(c *ServerCapabilities) { c.DeclarationProvider = true }}, "", onRequest(navigationHandler(navigation.ModeDeclaration))},
	{"textDocument/definition", clientRequest, supported, capability{"definitionProvider", func(c *ServerCapabilities) { c.DefinitionProvider = true }}, "", onRequest(navigationHandler(navigation.ModeDefinition))},
	{"textDocument/typeDefinition", clientRequest, supported, capability{"typeDefinitionProvider", func(c *ServerCapabilities) { c.TypeDefinitionProvider = true }}, "array and associative array declarations", onRequest(navigationHandler(navigation.ModeTypeDefinition))},
	{"textDocument/implementation", clientRequest, supported, capability{"implementationProvider", func(c *ServerCapabilities) { c.ImplementationProvider = true }}, "every definition of a function across the sourced chain and workspace", onRequest(navigationHandler(navigation.ModeImplementation))},
	{"textDocument/references", clientRequest, supported, capability{"referencesProvider", func(c *ServerCapabilities) { c.ReferencesProvider = true }}, "", onRequest((*Server).handleReferences)},
	{"textDocument/prepareCallHierarchy", clientRequest, supported, capability{"callHierarchyProvider", func(c *ServerCapabilities) { c.CallHierarchyProvider = true }}, "", onRequest((*Server).handlePrepareCallHierarchy)},
	{"callHierarchy/incomingCalls", clientRequest, supported, capability{"callHierarchyProvider", func(c *ServerCapabilities) { c.CallHierarchyProvider = true }}, "", onRequest((*Server).handleIncomingCalls)},
	{"callHierarchy/outgoingCalls", clientRequest, supported, capability{"callHierarchyProvider", func(c *ServerCapabilities) { c.CallHierarchyProvider = true }}, "", onRequest((*Server).handleOutgoingCalls)},
	{"textDocument/prepareTypeHierarchy", clientRequest, notAdvertised, capability{path: "typeHierarchyProvider"}, "shell has no types with supertypes or subtypes", nil},
	{"typeHierarchy/supertypes", clientRequest, notAdvertised, capability{path: "typeHierarchyProvider"}, "shell has no types with supertypes or subtypes", nil},
	{"typeHierarchy/subtypes", clientRequest, notAdvertised, capability{path: "typeHierarchyProvider"}, "shell has no types with supertypes or subtypes", nil},
	{"textDocument/documentHighlight", clientRequest, supported, capability{"documentHighlightProvider", func(c *ServerCapabilities) { c.DocumentHighlightProvider = true }}, "", onRequest((*Server).handleDocumentHighlight)},
	{"textDocument/documentLink", clientRequest, supported, capability{"documentLinkProvider", func(c *ServerCapabilities) { ensure(&c.DocumentLinkProvider) }}, "", onRequest((*Server).handleDocumentLink)},
	{"documentLink/resolve", clientRequest, notAdvertised, capability{path: "documentLinkProvider.resolveProvider"}, "links are complete when returned", nil},
	{"textDocument/hover", clientRequest, supported, capability{"hoverProvider", func(c *ServerCapabilities) { c.HoverProvider = true }}, "", onRequest((*Server).handleHover)},
	{"textDocument/codeLens", clientRequest, supported, capability{"codeLensProvider", func(c *ServerCapabilities) { ensure(&c.CodeLensProvider) }}, "", onRequest((*Server).handleCodeLens)},
	{"codeLens/resolve", clientRequest, supported, capability{"codeLensProvider.resolveProvider", func(c *ServerCapabilities) { ensure(&c.CodeLensProvider).ResolveProvider = true }}, "reference counts are computed on resolve; Bats run lenses arrive resolved", onRequest((*Server).handleCodeLensResolve)},
	{"textDocument/foldingRange", clientRequest, supported, capability{"foldingRangeProvider", func(c *ServerCapabilities) { c.FoldingRangeProvider = true }}, "", onRequest((*Server).handleFoldingRange)},
	{"textDocument/selectionRange", clientRequest, supported, capability{"selectionRangeProvider", func(c *ServerCapabilities) { c.SelectionRangeProvider = true }}, "", onRequest((*Server).handleSelectionRange)},
	{"textDocument/documentSymbol", clientRequest, supported, capability{"documentSymbolProvider", func(c *ServerCapabilities) { c.DocumentSymbolProvider = true }}, "", onRequest((*Server).handleDocumentSymbol)},
	{"textDocument/semanticTokens/full", clientRequest, supported, capability{"semanticTokensProvider.full", func(c *ServerCapabilities) { ensure(&semanticTokens(c).Full) }}, "", onRequest((*Server).handleSemanticTokensFull)},
	{"textDocument/semanticTokens/full/delta", clientRequest, supported, capability{"semanticTokensProvider.full.delta", func(c *ServerCapabilities) { ensure(&semanticTokens(c).Full).Delta = true }}, "", onRequest((*Server).handleSemanticTokensDelta)},
	{"textDocument/semanticTokens/range", clientRequest, supported, capability{"semanticTokensProvider.range", func(c *ServerCapabilities) { semanticTokens(c).Range = true }}, "", onRequest((*Server).handleSemanticTokensRange)},
	{"textDocument/inlineValue", clientRequest, supported, capability{"inlineValueProvider", func(c *ServerCapabilities) { c.InlineValueProvider = true }}, "", onRequest((*Server).handleInlineValue)},
	{"textDocument/inlayHint", clientRequest, supported, capability{"inlayHintProvider", func(c *ServerCapabilities) { ensure(&c.InlayHintProvider) }}, "", onRequest((*Server).handleInlayHint)},
	{"inlayHint/resolve", clientRequest, supported, capability{"inlayHintProvider.resolveProvider", func(c *ServerCapabilities) { ensure(&c.InlayHintProvider).ResolveProvider = true }}, "tooltips are filled in on resolve", onRequest((*Server).handleInlayHintResolve)},
	{"textDocument/moniker", clientRequest, supported, capability{"monikerProvider", func(c *ServerCapabilities) { c.MonikerProvider = true }}, "", onRequest((*Server).handleMoniker)},
	{"textDocument/completion", clientRequest, supported, capability{"completionProvider", func(c *ServerCapabilities) { ensure(&c.CompletionProvider).TriggerCharacters = completionTriggers }}, "", onRequest((*Server).handleCompletion)},
	{"completionItem/resolve", clientRequest, supported, capability{"completionProvider.resolveProvider", func(c *ServerCapabilities) { ensure(&c.CompletionProvider).ResolveProvider = true }}, "man page documentation for commands", onRequest((*Server).handleCompletionResolve)},
	{"textDocument/diagnostic", clientRequest, supported, capability{"diagnosticProvider", func(c *ServerCapabilities) { ensure(&c.DiagnosticProvider).InterFileDependencies = true }}, "", onRequest((*Server).handleDocumentDiagnostic)},
	{"workspace/diagnostic", clientRequest, supported, capability{"diagnosticProvider.workspaceDiagnostics", func(c *ServerCapabilities) { ensure(&c.DiagnosticProvider).WorkspaceDiagnostics = true }}, "lints every shell file under the workspace folders, reporting unchanged files by result id", onRequest((*Server).handleWorkspaceDiagnostic)},
	{"textDocument/signatureHelp", clientRequest, supported, capability{"signatureHelpProvider", func(c *ServerCapabilities) { c.SignatureHelpProvider = &signatureHelpOptions }}, "", onRequest((*Server).handleSignatureHelp)},
	{"textDocument/codeAction", clientRequest, supported, capability{"codeActionProvider", func(c *ServerCapabilities) { ensure(&c.CodeActionProvider).CodeActionKinds = actions.Kinds() }}, "edits that span files carry a needsConfirmation change annotation when the client supports them", onRequest((*Server).handleCodeAction)},
	{"codeAction/resolve", clientRequest, supported, capability{"codeActionProvider.resolveProvider", func(c *ServerCapabilities) { ensure(&c.CodeActionProvider).ResolveProvider = true }}, "refactor edits are computed on resolve", onRequest((*Server).handleCodeActionResolve)},
	{"textDocument/documentColor", clientRequest, notAdvertised, capability{path: "colorProvider"}, "shell has no color literals", nil},
	{"textDocument/colorPresentation", clientRequest, notAdvertised, capability{path: "colorProvider"}, "shell has no color literals", nil},
	{"textDocument/formatting", clientRequest, supported, capability{"documentFormattingProvider", func(c *ServerCapabilities) { c.DocumentFormattingProvider = true }}, "", onRequest((*Server).handleFormatting)},
	{"textDocument/rangeFormatting", clientRequest, supported, capability{"documentRangeFormattingProvider", func(c *ServerCapabilities) { ensure(&c.DocumentRangeFormattingProvider) }}, "", onRequest((*Server).handleRangeFormatting)},
	{"textDocument/rangesFormatting", clientRequest, supported, capability{"documentRangeFormattingProvider.rangesSupport", func(c *ServerCapabilities) { ensure(&c.DocumentRangeFormattingProvider).RangesSupport = true }}, "", onRequest((*Server).handleRangesFormatting)},
	{"textDocument/onTypeFormatting", clientRequest, notAdvertised, capability{path: "documentOnTypeFormattingProvider"}, "shfmt formats whole statements, not keystrokes", nil},
	{"textDocument/rename", clientRequest, supported, capability{"renameProvider", func(c *ServerCapabilities) { ensure(&c.RenameProvider) }}, "", onRequest((*Server).handleRename)},
	{"textDocument/prepareRename", clientRequest, supported, capability{"renameProvider.prepareProvider", func(c *ServerCapabilities) { ensure(&c.RenameProvider).PrepareProvider = true }}, "", onRequest((*Server).handlePrepareRename)},
	{"textDocument/linkedEditingRange", clientRequest, supported, capability{"linkedEditingRangeProvider", func(c *ServerCapabilities) { c.LinkedEditingRangeProvider = true }}, "heredoc delimiters", onRequest((*Server).handleLinkedEditingRange)},
	{"textDocument/inlineCompletion", clientRequest, notAdvertised, capability{path: "inlineCompletionProvider"}, "completion items cover the same ground", nil},
	{"workspace/symbol", clientRequest, supported, capability{"workspaceSymbolProvider", func(c *ServerCapabilities) { c.WorkspaceSymbolProvider = true }}, "", onRequest((*Server).handleWorkspaceSymbol)},
	{"workspaceSymbol/resolve", clientRequest, notAdvertised, capability{path: "workspaceSymbolProvider.resolveProvider"}, "symbols carry full locations", nil},
	{documentDialectMethod, clientRequest, supported, capability{}, "shell-lsp extension: the dialect a document is linted as, what decided it, and whether that was a guess", onRequest((*Server).handleDocumentDialect)},
	{setDialectMethod, clientRequest, supported, capability{}, "shell-lsp extension: the edit that sets or removes the document's # shellcheck shell= directive", onRequest((*Server).handleSetDialect)},
	{"workspace/executeCommand", clientRequest, supported, capability{"executeCommandProvider", func(c *ServerCapabilities) {
		c.ExecuteCommandProvider = &ExecuteCommandOptions{Commands: serverCommands}
	}}, strings.Join(serverCommands, ", "), onRequest((*Server).handleExecuteCommand)},
	{"workspace/willCreateFiles", clientRequest, notAdvertised, capability{path: "workspace.fileOperations.willCreate"}, "nothing to edit before a file exists", nil},
	{"workspace/willRenameFiles", clientRequest, supported, capability{"workspace.fileOperations.willRename", func(c *ServerCapabilities) { fileOperations(c).WillRename = &watchedFileOperations }}, "rewrites source paths and shebangs", onRequest((*Server).handleWillRenameFiles)},
	{"workspace/willDeleteFiles", clientRequest, notAdvertised, capability{path: "workspace.fileOperations.willDelete"}, "nothing to edit before a file is deleted", nil},
	{"workspace/textDocumentContent", clientRequest, notAdvertised, capability{path: "workspace.textDocumentContent"}, "the server owns no virtual documents", nil},

	{"initialized", clientNotification, supported, capability{}, "pulls configuration and registers file watchers", onNotification((*Server).handleInitialized)},
	{"exit", clientNotification, supported, capability{}, "exit code 0 after shutdown, 1 otherwise", nil},
	{"$/cancelRequest", clientNotification, supported, capability{}, "", onNotification((*Server).handleCancelRequest)},
	{"$/setTrace", clientNotification, supported, capability{}, "", onNotification((*Server).handleSetTrace)},
	{"window/workDoneProgress/cancel", clientNotification, supported, capability{}, "", onNotification((*Server).handleWorkDoneProgressCancel)},
	{"textDocument/didOpen", clientNotification, supported, capability{"textDocumentSync.openClose", func(c *ServerCapabilities) { ensure(&c.TextDocumentSync).OpenClose = true }}, "", onNotification((*Server).handleDidOpen)},
	{"textDocument/didChange", clientNotification, supported, capability{"textDocumentSync.change", func(c *ServerCapabilities) { ensure(&c.TextDocumentSync).Change = incrementalSync }}, "incremental", onNotification((*Server).handleDidChange)},
	{"textDocument/willSave", clientNotification, notAdvertised, capability{path: "textDocumentSync.willSave"}, "nothing depends on saves", nil},
	{"textDocument/didSave", clientNotification, notAdvertised, capability{path: "textDocumentSync.save"}, "the open document is authoritative; saves change nothing", nil},
	{"textDocument/didClose", clientNotification, supported, capability{"textDocumentSync.openClose", func(c *ServerCapabilities) { ensure(&c.TextDocumentSync).OpenClose = true }}, "", onNotification((*Server).handleDidClose)},
	{"notebookDocument/didOpen", clientNotification, notAdvertised, capability{path: "notebookDocumentSync"}, "no shell notebook format", nil},
	{"notebookDocument/didChange", clientNotification, notAdvertised, capability{path: "notebookDocumentSync"}, "no shell notebook format", nil},
	{"notebookDocument/didSave", clientNotification, notAdvertised, capability{path: "notebookDocumentSync"}, "no shell notebook format", nil},
	{"notebookDocument/didClose", clientNotification, notAdvertised, capability{path: "notebookDocumentSync"}, "no shell notebook format", nil},
	{"workspace/didChangeWorkspaceFolders", clientNotification, supported, capability{"workspace.workspaceFolders.changeNotifications", func(c *ServerCapabilities) {
		ensure(&c.Workspace).WorkspaceFolders = &workspaceFoldersCapability{Supported: true, ChangeNotifications: true}
	}}, "", onNotification((*Server).handleDidChangeWorkspaceFolders)},
	{"workspace/didChangeConfiguration", clientNotification, supported, capability{}, "", onNotification((*Server).handleDidChangeConfiguration)},
	{didChangeWatchedFilesMethod, clientNotification, supported, capability{}, "watchers are registered dynamically", onNotification((*Server).handleDidChangeWatchedFiles)},
	{"workspace/didCreateFiles", clientNotification, supported, capability{"workspace.fileOperations.didCreate", func(c *ServerCapabilities) { fileOperations(c).DidCreate = &watchedFileOperations }}, "", onNotification((*Server).handleFilesChanged)},
	{"workspace/didRenameFiles", clientNotification, supported, capability{"workspace.fileOperations.didRename", func(c *ServerCapabilities) { fileOperations(c).DidRename = &watchedFileOperations }}, "", onNotification((*Server).handleDidRenameFiles)},
	{"workspace/didDeleteFiles", clientNotification, supported, capability{"workspace.fileOperations.didDelete", func(c *ServerCapabilities) { fileOperations(c).DidDelete = &watchedFileOperations }}, "", onNotification((*Server).handleFilesChanged)},

	{progressCreateMethod, serverRequest, sent, capability{}, "workspace indexing and Bats run progress", nil},
	{"window/showMessageRequest", serverRequest, notSent, capability{}, "the server never needs a decision from the user", nil},
	{"window/showDocument", serverRequest, notSent, capability{}, "", nil},
	{registerCapabilityMethod, serverRequest, sent, capability{}, "file watchers", nil},
	{"client/unregisterCapability", serverRequest, notSent, capability{}, "registrations live for the whole session", nil},
	{"workspace/workspaceFolders", serverRequest, notSent, capability{}, "folders come from initialize and change notifications", nil},
	{configurationMethod, serverRequest, sent, capability{}, "", nil},
	{applyEditMethod, serverRequest, sent, capability{}, "", nil},
	{codeLensRefreshMethod, serverRequest, sent, capability{}, "after settings change", nil},
	{semanticTokensRefreshMethod, serverRequest, sent, capability{}, "after settings change", nil},
	{inlayHintRefreshMethod, serverRequest, sent, capability{}, "after settings change", nil},
	{"workspace/inlineValue/refresh", serverRequest, notSent, capability{}, "inline values are computed per request", nil},
	{diagnosticRefreshMethod, serverRequest, sent, capability{}, "after settings change and after a Bats run", nil},
	{"workspace/foldingRange/refresh", serverRequest, notSent, capability{}, "folding depends only on the document", nil},
	{"workspace/textDocumentContent/refresh", serverRequest, notSent, capability{}, "the server owns no virtual documents", nil},

	{showMessageMethod, serverNotification, sent, capability{}, "rejected configuration and Bats run summaries", nil},
	{logMessageMethod, serverNotification, sent, capability{}, "one-time notices such as a failed file watcher registration", nil},
	{"telemetry/event", serverNotification, notSent, capability{}, "the server collects no telemetry", nil},
	{publishDiagnosticsMethod, serverNotification, sent, capability{}, "only for clients without pull diagnostics", nil},
	{progressMethod, serverNotification, sent, capability{}, "", nil},
	{logTraceMethod, serverNotification, sent, capability{}, "when $/setTrace enables tracing", nil},
}
