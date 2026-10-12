package server

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

type method struct {
	name       string
	direction  direction
	support    support
	capability string
	note       string
}

var methods = []method{
	{"initialize", clientRequest, supported, "", "negotiates position encoding, pull or push diagnostics, workspace folders and settings"},
	{"shutdown", clientRequest, supported, "", "later requests fail with InvalidRequest"},
	{"textDocument/willSaveWaitUntil", clientRequest, notAdvertised, "textDocumentSync.willSaveWaitUntil", "formatting is offered through textDocument/formatting instead"},
	{"textDocument/declaration", clientRequest, supported, "declarationProvider", ""},
	{"textDocument/definition", clientRequest, supported, "definitionProvider", ""},
	{"textDocument/typeDefinition", clientRequest, supported, "typeDefinitionProvider", "array and associative array declarations"},
	{"textDocument/implementation", clientRequest, supported, "implementationProvider", "every definition of a function across the sourced chain and workspace"},
	{"textDocument/references", clientRequest, supported, "referencesProvider", ""},
	{"textDocument/prepareCallHierarchy", clientRequest, supported, "callHierarchyProvider", ""},
	{"callHierarchy/incomingCalls", clientRequest, supported, "callHierarchyProvider", ""},
	{"callHierarchy/outgoingCalls", clientRequest, supported, "callHierarchyProvider", ""},
	{"textDocument/prepareTypeHierarchy", clientRequest, notAdvertised, "typeHierarchyProvider", "shell has no types with supertypes or subtypes"},
	{"typeHierarchy/supertypes", clientRequest, notAdvertised, "typeHierarchyProvider", "shell has no types with supertypes or subtypes"},
	{"typeHierarchy/subtypes", clientRequest, notAdvertised, "typeHierarchyProvider", "shell has no types with supertypes or subtypes"},
	{"textDocument/documentHighlight", clientRequest, supported, "documentHighlightProvider", ""},
	{"textDocument/documentLink", clientRequest, supported, "documentLinkProvider", ""},
	{"documentLink/resolve", clientRequest, notAdvertised, "documentLinkProvider.resolveProvider", "links are complete when returned"},
	{"textDocument/hover", clientRequest, supported, "hoverProvider", ""},
	{"textDocument/codeLens", clientRequest, supported, "codeLensProvider", ""},
	{"codeLens/resolve", clientRequest, supported, "codeLensProvider.resolveProvider", "reference counts are computed on resolve; Bats run lenses arrive resolved"},
	{"textDocument/foldingRange", clientRequest, supported, "foldingRangeProvider", ""},
	{"textDocument/selectionRange", clientRequest, supported, "selectionRangeProvider", ""},
	{"textDocument/documentSymbol", clientRequest, supported, "documentSymbolProvider", ""},
	{"textDocument/semanticTokens/full", clientRequest, supported, "semanticTokensProvider.full", ""},
	{"textDocument/semanticTokens/full/delta", clientRequest, supported, "semanticTokensProvider.full.delta", ""},
	{"textDocument/semanticTokens/range", clientRequest, supported, "semanticTokensProvider.range", ""},
	{"textDocument/inlineValue", clientRequest, supported, "inlineValueProvider", ""},
	{"textDocument/inlayHint", clientRequest, supported, "inlayHintProvider", ""},
	{"inlayHint/resolve", clientRequest, supported, "inlayHintProvider.resolveProvider", "tooltips are filled in on resolve"},
	{"textDocument/moniker", clientRequest, supported, "monikerProvider", ""},
	{"textDocument/completion", clientRequest, supported, "completionProvider", ""},
	{"completionItem/resolve", clientRequest, supported, "completionProvider.resolveProvider", "man page documentation for commands"},
	{"textDocument/diagnostic", clientRequest, supported, "diagnosticProvider", ""},
	{"workspace/diagnostic", clientRequest, supported, "diagnosticProvider.workspaceDiagnostics", "lints every shell file under the workspace folders, reporting unchanged files by result id"},
	{"textDocument/signatureHelp", clientRequest, supported, "signatureHelpProvider", ""},
	{"textDocument/codeAction", clientRequest, supported, "codeActionProvider", "edits that span files carry a needsConfirmation change annotation when the client supports them"},
	{"codeAction/resolve", clientRequest, supported, "codeActionProvider.resolveProvider", "refactor edits are computed on resolve"},
	{"textDocument/documentColor", clientRequest, notAdvertised, "colorProvider", "shell has no color literals"},
	{"textDocument/colorPresentation", clientRequest, notAdvertised, "colorProvider", "shell has no color literals"},
	{"textDocument/formatting", clientRequest, supported, "documentFormattingProvider", ""},
	{"textDocument/rangeFormatting", clientRequest, supported, "documentRangeFormattingProvider", ""},
	{"textDocument/rangesFormatting", clientRequest, supported, "documentRangeFormattingProvider.rangesSupport", ""},
	{"textDocument/onTypeFormatting", clientRequest, notAdvertised, "documentOnTypeFormattingProvider", "shfmt formats whole statements, not keystrokes"},
	{"textDocument/rename", clientRequest, supported, "renameProvider", ""},
	{"textDocument/prepareRename", clientRequest, supported, "renameProvider.prepareProvider", ""},
	{"textDocument/linkedEditingRange", clientRequest, supported, "linkedEditingRangeProvider", "heredoc delimiters"},
	{"textDocument/inlineCompletion", clientRequest, notAdvertised, "inlineCompletionProvider", "completion items cover the same ground"},
	{"workspace/symbol", clientRequest, supported, "workspaceSymbolProvider", ""},
	{"workspaceSymbol/resolve", clientRequest, notAdvertised, "workspaceSymbolProvider.resolveProvider", "symbols carry full locations"},
	{documentDialectMethod, clientRequest, supported, "", "shell-lsp extension: the dialect a document is linted as, what decided it, and whether that was a guess"},
	{setDialectMethod, clientRequest, supported, "", "shell-lsp extension: the edit that sets or removes the document's # shellcheck shell= directive"},
	{"workspace/executeCommand", clientRequest, supported, "executeCommandProvider", "shellLsp.fixAll, shellLsp.applyEdit, shellLsp.showReferences, shellLsp.runBats"},
	{"workspace/willCreateFiles", clientRequest, notAdvertised, "workspace.fileOperations.willCreate", "nothing to edit before a file exists"},
	{"workspace/willRenameFiles", clientRequest, supported, "workspace.fileOperations.willRename", "rewrites source paths and shebangs"},
	{"workspace/willDeleteFiles", clientRequest, notAdvertised, "workspace.fileOperations.willDelete", "nothing to edit before a file is deleted"},
	{"workspace/textDocumentContent", clientRequest, notAdvertised, "workspace.textDocumentContent", "the server owns no virtual documents"},

	{"initialized", clientNotification, supported, "", "pulls configuration and registers file watchers"},
	{"exit", clientNotification, supported, "", "exit code 0 after shutdown, 1 otherwise"},
	{"$/cancelRequest", clientNotification, supported, "", ""},
	{"$/setTrace", clientNotification, supported, "", ""},
	{"window/workDoneProgress/cancel", clientNotification, supported, "", ""},
	{"textDocument/didOpen", clientNotification, supported, "textDocumentSync.openClose", ""},
	{"textDocument/didChange", clientNotification, supported, "textDocumentSync.change", "incremental"},
	{"textDocument/willSave", clientNotification, notAdvertised, "textDocumentSync.willSave", "nothing depends on saves"},
	{"textDocument/didSave", clientNotification, notAdvertised, "textDocumentSync.save", "the open document is authoritative; saves change nothing"},
	{"textDocument/didClose", clientNotification, supported, "textDocumentSync.openClose", ""},
	{"notebookDocument/didOpen", clientNotification, notAdvertised, "notebookDocumentSync", "no shell notebook format"},
	{"notebookDocument/didChange", clientNotification, notAdvertised, "notebookDocumentSync", "no shell notebook format"},
	{"notebookDocument/didSave", clientNotification, notAdvertised, "notebookDocumentSync", "no shell notebook format"},
	{"notebookDocument/didClose", clientNotification, notAdvertised, "notebookDocumentSync", "no shell notebook format"},
	{"workspace/didChangeWorkspaceFolders", clientNotification, supported, "workspace.workspaceFolders.changeNotifications", ""},
	{"workspace/didChangeConfiguration", clientNotification, supported, "", ""},
	{didChangeWatchedFilesMethod, clientNotification, supported, "", "watchers are registered dynamically"},
	{"workspace/didCreateFiles", clientNotification, supported, "workspace.fileOperations.didCreate", ""},
	{"workspace/didRenameFiles", clientNotification, supported, "workspace.fileOperations.didRename", ""},
	{"workspace/didDeleteFiles", clientNotification, supported, "workspace.fileOperations.didDelete", ""},

	{progressCreateMethod, serverRequest, sent, "", "workspace indexing and Bats run progress"},
	{"window/showMessageRequest", serverRequest, notSent, "", "the server never needs a decision from the user"},
	{"window/showDocument", serverRequest, notSent, "", ""},
	{registerCapabilityMethod, serverRequest, sent, "", "file watchers"},
	{"client/unregisterCapability", serverRequest, notSent, "", "registrations live for the whole session"},
	{"workspace/workspaceFolders", serverRequest, notSent, "", "folders come from initialize and change notifications"},
	{configurationMethod, serverRequest, sent, "", ""},
	{applyEditMethod, serverRequest, sent, "", ""},
	{codeLensRefreshMethod, serverRequest, sent, "", "after settings change"},
	{semanticTokensRefreshMethod, serverRequest, sent, "", "after settings change"},
	{inlayHintRefreshMethod, serverRequest, sent, "", "after settings change"},
	{"workspace/inlineValue/refresh", serverRequest, notSent, "", "inline values are computed per request"},
	{diagnosticRefreshMethod, serverRequest, sent, "", "after settings change and after a Bats run"},
	{"workspace/foldingRange/refresh", serverRequest, notSent, "", "folding depends only on the document"},
	{"workspace/textDocumentContent/refresh", serverRequest, notSent, "", "the server owns no virtual documents"},

	{showMessageMethod, serverNotification, sent, "", "rejected configuration and Bats run summaries"},
	{logMessageMethod, serverNotification, sent, "", "one-time notices such as a failed file watcher registration"},
	{"telemetry/event", serverNotification, notSent, "", "the server collects no telemetry"},
	{publishDiagnosticsMethod, serverNotification, sent, "", "only for clients without pull diagnostics"},
	{progressMethod, serverNotification, sent, "", ""},
	{logTraceMethod, serverNotification, sent, "", "when $/setTrace enables tracing"},
}
