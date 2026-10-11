# LSP method coverage

Generated from `internal/server/methods.go` by `go test ./internal/server -run TestMethodDocs -update`.
The conformance tests in `methods_test.go` check that every implemented method has a handler and an
advertised capability, and that nothing else is handled or advertised.

## Client → server requests

| Method | Status | Capability | Notes |
| --- | --- | --- | --- |
| `initialize` | implemented |  | negotiates position encoding, pull or push diagnostics, workspace folders and settings |
| `shutdown` | implemented |  | later requests fail with InvalidRequest |
| `textDocument/willSaveWaitUntil` | not advertised | `textDocumentSync.willSaveWaitUntil` | formatting is offered through textDocument/formatting instead |
| `textDocument/declaration` | implemented | `declarationProvider` |  |
| `textDocument/definition` | implemented | `definitionProvider` |  |
| `textDocument/typeDefinition` | implemented | `typeDefinitionProvider` | array and associative array declarations |
| `textDocument/implementation` | implemented | `implementationProvider` | every definition of a function across the sourced chain and workspace |
| `textDocument/references` | implemented | `referencesProvider` |  |
| `textDocument/prepareCallHierarchy` | implemented | `callHierarchyProvider` |  |
| `callHierarchy/incomingCalls` | implemented | `callHierarchyProvider` |  |
| `callHierarchy/outgoingCalls` | implemented | `callHierarchyProvider` |  |
| `textDocument/prepareTypeHierarchy` | not advertised | `typeHierarchyProvider` | shell has no types with supertypes or subtypes |
| `typeHierarchy/supertypes` | not advertised | `typeHierarchyProvider` | shell has no types with supertypes or subtypes |
| `typeHierarchy/subtypes` | not advertised | `typeHierarchyProvider` | shell has no types with supertypes or subtypes |
| `textDocument/documentHighlight` | implemented | `documentHighlightProvider` |  |
| `textDocument/documentLink` | implemented | `documentLinkProvider` |  |
| `documentLink/resolve` | not advertised | `documentLinkProvider.resolveProvider` | links are complete when returned |
| `textDocument/hover` | implemented | `hoverProvider` |  |
| `textDocument/codeLens` | implemented | `codeLensProvider` |  |
| `codeLens/resolve` | implemented | `codeLensProvider.resolveProvider` | reference counts are computed on resolve; Bats run lenses arrive resolved |
| `textDocument/foldingRange` | implemented | `foldingRangeProvider` |  |
| `textDocument/selectionRange` | implemented | `selectionRangeProvider` |  |
| `textDocument/documentSymbol` | implemented | `documentSymbolProvider` |  |
| `textDocument/semanticTokens/full` | implemented | `semanticTokensProvider.full` |  |
| `textDocument/semanticTokens/full/delta` | implemented | `semanticTokensProvider.full.delta` |  |
| `textDocument/semanticTokens/range` | implemented | `semanticTokensProvider.range` |  |
| `textDocument/inlineValue` | implemented | `inlineValueProvider` |  |
| `textDocument/inlayHint` | implemented | `inlayHintProvider` |  |
| `inlayHint/resolve` | implemented | `inlayHintProvider.resolveProvider` | tooltips are filled in on resolve |
| `textDocument/moniker` | implemented | `monikerProvider` |  |
| `textDocument/completion` | implemented | `completionProvider` |  |
| `completionItem/resolve` | implemented | `completionProvider.resolveProvider` | man page documentation for commands |
| `textDocument/diagnostic` | implemented | `diagnosticProvider` |  |
| `workspace/diagnostic` | implemented | `diagnosticProvider.workspaceDiagnostics` | lints every shell file under the workspace folders, reporting unchanged files by result id |
| `textDocument/signatureHelp` | implemented | `signatureHelpProvider` |  |
| `textDocument/codeAction` | implemented | `codeActionProvider` |  |
| `codeAction/resolve` | implemented | `codeActionProvider.resolveProvider` | refactor edits are computed on resolve |
| `textDocument/documentColor` | not advertised | `colorProvider` | shell has no color literals |
| `textDocument/colorPresentation` | not advertised | `colorProvider` | shell has no color literals |
| `textDocument/formatting` | implemented | `documentFormattingProvider` |  |
| `textDocument/rangeFormatting` | implemented | `documentRangeFormattingProvider` |  |
| `textDocument/rangesFormatting` | implemented | `documentRangeFormattingProvider.rangesSupport` |  |
| `textDocument/onTypeFormatting` | not advertised | `documentOnTypeFormattingProvider` | shfmt formats whole statements, not keystrokes |
| `textDocument/rename` | implemented | `renameProvider` |  |
| `textDocument/prepareRename` | implemented | `renameProvider.prepareProvider` |  |
| `textDocument/linkedEditingRange` | implemented | `linkedEditingRangeProvider` | heredoc delimiters |
| `textDocument/inlineCompletion` | not advertised | `inlineCompletionProvider` | completion items cover the same ground |
| `workspace/symbol` | implemented | `workspaceSymbolProvider` |  |
| `workspaceSymbol/resolve` | not advertised | `workspaceSymbolProvider.resolveProvider` | symbols carry full locations |
| `workspace/executeCommand` | implemented | `executeCommandProvider` | shellLsp.fixAll, shellLsp.applyEdit, shellLsp.showReferences, shellLsp.runBats |
| `workspace/willCreateFiles` | not advertised | `workspace.fileOperations.willCreate` | nothing to edit before a file exists |
| `workspace/willRenameFiles` | implemented | `workspace.fileOperations.willRename` | rewrites source paths and shebangs |
| `workspace/willDeleteFiles` | not advertised | `workspace.fileOperations.willDelete` | nothing to edit before a file is deleted |
| `workspace/textDocumentContent` | not advertised | `workspace.textDocumentContent` | the server owns no virtual documents |

## Client → server notifications

| Method | Status | Capability | Notes |
| --- | --- | --- | --- |
| `initialized` | implemented |  | pulls configuration and registers file watchers |
| `exit` | implemented |  | exit code 0 after shutdown, 1 otherwise |
| `$/cancelRequest` | implemented |  |  |
| `$/setTrace` | implemented |  |  |
| `window/workDoneProgress/cancel` | implemented |  |  |
| `textDocument/didOpen` | implemented | `textDocumentSync.openClose` |  |
| `textDocument/didChange` | implemented | `textDocumentSync.change` | incremental |
| `textDocument/willSave` | not advertised | `textDocumentSync.willSave` | nothing depends on saves |
| `textDocument/didSave` | not advertised | `textDocumentSync.save` | the open document is authoritative; saves change nothing |
| `textDocument/didClose` | implemented | `textDocumentSync.openClose` |  |
| `notebookDocument/didOpen` | not advertised | `notebookDocumentSync` | no shell notebook format |
| `notebookDocument/didChange` | not advertised | `notebookDocumentSync` | no shell notebook format |
| `notebookDocument/didSave` | not advertised | `notebookDocumentSync` | no shell notebook format |
| `notebookDocument/didClose` | not advertised | `notebookDocumentSync` | no shell notebook format |
| `workspace/didChangeWorkspaceFolders` | implemented | `workspace.workspaceFolders.changeNotifications` |  |
| `workspace/didChangeConfiguration` | implemented |  |  |
| `workspace/didChangeWatchedFiles` | implemented |  | watchers are registered dynamically |
| `workspace/didCreateFiles` | implemented | `workspace.fileOperations.didCreate` |  |
| `workspace/didRenameFiles` | implemented | `workspace.fileOperations.didRename` |  |
| `workspace/didDeleteFiles` | implemented | `workspace.fileOperations.didDelete` |  |

## Server → client requests

| Method | Status | Capability | Notes |
| --- | --- | --- | --- |
| `window/workDoneProgress/create` | sent |  | workspace indexing and Bats run progress |
| `window/showMessageRequest` | never sent |  | the server never needs a decision from the user |
| `window/showDocument` | never sent |  |  |
| `client/registerCapability` | sent |  | file watchers |
| `client/unregisterCapability` | never sent |  | registrations live for the whole session |
| `workspace/workspaceFolders` | never sent |  | folders come from initialize and change notifications |
| `workspace/configuration` | sent |  |  |
| `workspace/applyEdit` | sent |  |  |
| `workspace/codeLens/refresh` | sent |  | after settings change |
| `workspace/semanticTokens/refresh` | sent |  | after settings change |
| `workspace/inlayHint/refresh` | sent |  | after settings change |
| `workspace/inlineValue/refresh` | never sent |  | inline values are computed per request |
| `workspace/diagnostic/refresh` | sent |  | after settings change and after a Bats run |
| `workspace/foldingRange/refresh` | never sent |  | folding depends only on the document |
| `workspace/textDocumentContent/refresh` | never sent |  | the server owns no virtual documents |

## Server → client notifications

| Method | Status | Capability | Notes |
| --- | --- | --- | --- |
| `window/showMessage` | sent |  | rejected configuration and Bats run summaries |
| `window/logMessage` | sent |  | one-time notices such as a failed file watcher registration |
| `telemetry/event` | never sent |  | the server collects no telemetry |
| `textDocument/publishDiagnostics` | sent |  | only for clients without pull diagnostics |
| `$/progress` | sent |  |  |
| `$/logTrace` | sent |  | when $/setTrace enables tracing |
