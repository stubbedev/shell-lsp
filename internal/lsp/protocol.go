// Package lsp holds the subset of Language Server Protocol types the
// server exchanges with clients. Types are hand-rolled so the protocol
// surface stays exactly as large as what the server implements.
package lsp

import "encoding/json"

type Position struct {
	Line      uint32 `json:"line"`
	Character uint32 `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int32  `json:"version"`
}

type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int32  `json:"version"`
	Text       string `json:"text"`
}

type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

type TextDocumentContentChangeEvent struct {
	Range       *Range  `json:"range"`
	RangeLength *uint32 `json:"rangeLength,omitempty"`
	Text        string  `json:"text"`
}

type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

func DecodeParams[T any](raw json.RawMessage) (T, error) {
	var params T
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return params, err
		}
	}
	return params, nil
}

type DiagnosticSeverity uint8

const (
	SeverityError       DiagnosticSeverity = 1
	SeverityWarning     DiagnosticSeverity = 2
	SeverityInformation DiagnosticSeverity = 3
	SeverityHint        DiagnosticSeverity = 4
)

type DiagnosticTag uint8

const (
	TagUnnecessary DiagnosticTag = 1
	TagDeprecated  DiagnosticTag = 2
)

type CodeDescription struct {
	Href string `json:"href"`
}

type Diagnostic struct {
	Range           Range              `json:"range"`
	Severity        DiagnosticSeverity `json:"severity"`
	Code            string             `json:"code,omitempty"`
	CodeDescription *CodeDescription   `json:"codeDescription,omitempty"`
	Source          string             `json:"source"`
	Message         string             `json:"message"`
	Tags            []DiagnosticTag    `json:"tags,omitempty"`
	Data            json.RawMessage    `json:"data,omitempty"`
}

type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Version     *int32       `json:"version,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type DocumentDiagnosticParams struct {
	TextDocument     TextDocumentIdentifier `json:"textDocument"`
	Identifier       string                 `json:"identifier,omitempty"`
	PreviousResultID string                 `json:"previousResultId,omitempty"`
}

type FullDocumentDiagnosticReport struct {
	Kind     string       `json:"kind"`
	ResultID string       `json:"resultId"`
	Items    []Diagnostic `json:"items"`
}

type UnchangedDocumentDiagnosticReport struct {
	Kind     string `json:"kind"`
	ResultID string `json:"resultId"`
}

const (
	ReportFull      = "full"
	ReportUnchanged = "unchanged"
)

type PreviousResultID struct {
	URI   string `json:"uri"`
	Value string `json:"value"`
}

type WorkspaceDiagnosticParams struct {
	Identifier        string             `json:"identifier,omitempty"`
	PreviousResultIDs []PreviousResultID `json:"previousResultIds"`
}

type WorkspaceDocumentDiagnosticReport struct {
	Kind     string       `json:"kind"`
	ResultID string       `json:"resultId"`
	URI      string       `json:"uri"`
	Version  *int32       `json:"version"`
	Items    []Diagnostic `json:"items"`
}

type workspaceUnchangedDocumentDiagnosticReport struct {
	Kind     string `json:"kind"`
	ResultID string `json:"resultId"`
	URI      string `json:"uri"`
	Version  *int32 `json:"version"`
}

func (r WorkspaceDocumentDiagnosticReport) MarshalJSON() ([]byte, error) {
	if r.Kind == ReportUnchanged {
		return json.Marshal(workspaceUnchangedDocumentDiagnosticReport{Kind: r.Kind, ResultID: r.ResultID, URI: r.URI, Version: r.Version})
	}
	type fullReport WorkspaceDocumentDiagnosticReport
	if r.Items == nil {
		r.Items = []Diagnostic{}
	}
	return json.Marshal(fullReport(r))
}

type WorkspaceDiagnosticReport struct {
	Items []WorkspaceDocumentDiagnosticReport `json:"items"`
}

type DidChangeConfigurationParams struct {
	Settings json.RawMessage `json:"settings"`
}

type MessageType uint8

const (
	MessageError   MessageType = 1
	MessageWarning MessageType = 2
	MessageInfo    MessageType = 3
	MessageLog     MessageType = 4
)

type LogMessageParams struct {
	Type    MessageType `json:"type"`
	Message string      `json:"message"`
}

type ShowMessageParams struct {
	Type    MessageType `json:"type"`
	Message string      `json:"message"`
}

type TextEdit struct {
	Range        Range  `json:"range"`
	NewText      string `json:"newText"`
	AnnotationID string `json:"annotationId,omitempty"`
}

type ChangeAnnotation struct {
	Label             string `json:"label"`
	NeedsConfirmation bool   `json:"needsConfirmation,omitempty"`
}

const (
	MarkupMarkdown  = "markdown"
	MarkupPlainText = "plaintext"
)

type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type HoverParams struct {
	TextDocumentPositionParams
}

type Hover struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

type CompletionItemKind uint8

const (
	CompletionMethod     CompletionItemKind = 2
	CompletionFunction   CompletionItemKind = 3
	CompletionField      CompletionItemKind = 5
	CompletionVariable   CompletionItemKind = 6
	CompletionModule     CompletionItemKind = 9
	CompletionProperty   CompletionItemKind = 10
	CompletionValue      CompletionItemKind = 12
	CompletionKeyword    CompletionItemKind = 14
	CompletionSnippet    CompletionItemKind = 15
	CompletionFile       CompletionItemKind = 17
	CompletionFolder     CompletionItemKind = 19
	CompletionEnumMember CompletionItemKind = 20
	CompletionConstant   CompletionItemKind = 21
	CompletionOperator   CompletionItemKind = 24
)

type InsertTextFormat uint8

const (
	InsertPlainText InsertTextFormat = 1
	InsertSnippet   InsertTextFormat = 2
)

type CompletionParams struct {
	TextDocumentPositionParams
}

type CompletionItem struct {
	Label            string             `json:"label"`
	Kind             CompletionItemKind `json:"kind"`
	Detail           string             `json:"detail,omitempty"`
	Documentation    *MarkupContent     `json:"documentation,omitempty"`
	SortText         string             `json:"sortText,omitempty"`
	FilterText       string             `json:"filterText,omitempty"`
	InsertTextFormat InsertTextFormat   `json:"insertTextFormat"`
	TextEdit         *TextEdit          `json:"textEdit,omitempty"`
	Data             json.RawMessage    `json:"data,omitempty"`
}

type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

type WorkspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

type FileChangeType uint8

const (
	FileCreated FileChangeType = 1
	FileChanged FileChangeType = 2
	FileDeleted FileChangeType = 3
)

type FileEvent struct {
	URI  string         `json:"uri"`
	Type FileChangeType `json:"type"`
}

type DidChangeWatchedFilesParams struct {
	Changes []FileEvent `json:"changes"`
}

type SymbolKind uint8

const (
	SymbolFile     SymbolKind = 1
	SymbolMethod   SymbolKind = 6
	SymbolFunction SymbolKind = 12
	SymbolVariable SymbolKind = 13
	SymbolConstant SymbolKind = 14
	SymbolString   SymbolKind = 15
	SymbolArray    SymbolKind = 18
)

type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           SymbolKind       `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

type SymbolInformation struct {
	Name          string     `json:"name"`
	Kind          SymbolKind `json:"kind"`
	Location      Location   `json:"location"`
	ContainerName string     `json:"containerName,omitempty"`
}

type DocumentSymbolParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type FoldingRangeParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

const (
	FoldingComment = "comment"
	FoldingImports = "imports"
	FoldingRegion  = "region"
)

type FoldingRange struct {
	StartLine uint32 `json:"startLine"`
	EndLine   uint32 `json:"endLine"`
	Kind      string `json:"kind,omitempty"`
}

type WorkspaceSymbolParams struct {
	Query string `json:"query"`
}

type FormattingOptions struct {
	TabSize      uint `json:"tabSize"`
	InsertSpaces bool `json:"insertSpaces"`
}

type DocumentFormattingParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Options      FormattingOptions      `json:"options"`
}

type DocumentRangeFormattingParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
	Options      FormattingOptions      `json:"options"`
}

type CodeActionContext struct {
	Diagnostics []Diagnostic `json:"diagnostics"`
	Only        []string     `json:"only,omitempty"`
}

type CodeActionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
	Context      CodeActionContext      `json:"context"`
}

type OptionalVersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version *int32 `json:"version"`
}

type TextDocumentEdit struct {
	TextDocument OptionalVersionedTextDocumentIdentifier `json:"textDocument"`
	Edits        []TextEdit                              `json:"edits"`
}

type WorkspaceEdit struct {
	Changes           map[string][]TextEdit       `json:"changes,omitempty"`
	DocumentChanges   []TextDocumentEdit          `json:"documentChanges,omitempty"`
	ChangeAnnotations map[string]ChangeAnnotation `json:"changeAnnotations,omitempty"`
}

type Command struct {
	Title     string            `json:"title"`
	Command   string            `json:"command"`
	Arguments []json.RawMessage `json:"arguments,omitempty"`
}

type CodeAction struct {
	Title       string          `json:"title"`
	Kind        string          `json:"kind,omitempty"`
	Diagnostics []Diagnostic    `json:"diagnostics,omitempty"`
	IsPreferred bool            `json:"isPreferred,omitempty"`
	Edit        *WorkspaceEdit  `json:"edit,omitempty"`
	Command     *Command        `json:"command,omitempty"`
	Data        json.RawMessage `json:"data,omitempty"`
}

type ExecuteCommandParams struct {
	Command   string            `json:"command"`
	Arguments []json.RawMessage `json:"arguments,omitempty"`
}

type ApplyWorkspaceEditParams struct {
	Label string        `json:"label,omitempty"`
	Edit  WorkspaceEdit `json:"edit"`
}

type ApplyWorkspaceEditResult struct {
	Applied       bool   `json:"applied"`
	FailureReason string `json:"failureReason,omitempty"`
}

type ReferenceParams struct {
	TextDocumentPositionParams
	Context struct {
		IncludeDeclaration bool `json:"includeDeclaration"`
	} `json:"context"`
}

type RenameParams struct {
	TextDocumentPositionParams
	NewName string `json:"newName"`
}

type PrepareRenameResult struct {
	Range       Range  `json:"range"`
	Placeholder string `json:"placeholder"`
}

type DocumentHighlightKind uint8

const (
	HighlightRead  DocumentHighlightKind = 2
	HighlightWrite DocumentHighlightKind = 3
)

type DocumentHighlight struct {
	Range Range                 `json:"range"`
	Kind  DocumentHighlightKind `json:"kind"`
}

type SemanticTokensParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type SemanticTokensDeltaParams struct {
	TextDocument     TextDocumentIdentifier `json:"textDocument"`
	PreviousResultID string                 `json:"previousResultId"`
}

type SemanticTokensRangeParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
}

type SemanticTokens struct {
	ResultID string   `json:"resultId,omitempty"`
	Data     []uint32 `json:"data"`
}

type SemanticTokensEdit struct {
	Start       uint32   `json:"start"`
	DeleteCount uint32   `json:"deleteCount"`
	Data        []uint32 `json:"data"`
}

type SemanticTokensDelta struct {
	ResultID string               `json:"resultId"`
	Edits    []SemanticTokensEdit `json:"edits"`
}

type SelectionRangeParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Positions    []Position             `json:"positions"`
}

type SelectionRange struct {
	Range  Range           `json:"range"`
	Parent *SelectionRange `json:"parent,omitempty"`
}

type ParameterInformation struct {
	Label         [2]uint32      `json:"label"`
	Documentation *MarkupContent `json:"documentation,omitempty"`
}

type SignatureInformation struct {
	Label           string                 `json:"label"`
	Documentation   *MarkupContent         `json:"documentation,omitempty"`
	Parameters      []ParameterInformation `json:"parameters"`
	ActiveParameter *uint32                `json:"activeParameter,omitempty"`
}

type SignatureHelp struct {
	Signatures      []SignatureInformation `json:"signatures"`
	ActiveSignature uint32                 `json:"activeSignature"`
	ActiveParameter *uint32                `json:"activeParameter,omitempty"`
}

type InlayHintParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
}

type InlayHintKind uint8

const (
	InlayHintType      InlayHintKind = 1
	InlayHintParameter InlayHintKind = 2
)

type InlayHint struct {
	Position     Position        `json:"position"`
	Label        string          `json:"label"`
	Kind         InlayHintKind   `json:"kind,omitempty"`
	Tooltip      *MarkupContent  `json:"tooltip,omitempty"`
	PaddingLeft  bool            `json:"paddingLeft,omitempty"`
	PaddingRight bool            `json:"paddingRight,omitempty"`
	Data         json.RawMessage `json:"data,omitempty"`
}

type DocumentLinkParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type DocumentLink struct {
	Range  Range  `json:"range"`
	Target string `json:"target"`
}

type CodeLensParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type CodeLens struct {
	Range   Range           `json:"range"`
	Command *Command        `json:"command,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type CallHierarchyItem struct {
	Name           string          `json:"name"`
	Kind           SymbolKind      `json:"kind"`
	Detail         string          `json:"detail,omitempty"`
	URI            string          `json:"uri"`
	Range          Range           `json:"range"`
	SelectionRange Range           `json:"selectionRange"`
	Data           json.RawMessage `json:"data,omitempty"`
}

type CallHierarchyCallsParams struct {
	Item CallHierarchyItem `json:"item"`
}

type CallHierarchyIncomingCall struct {
	From       CallHierarchyItem `json:"from"`
	FromRanges []Range           `json:"fromRanges"`
}

type CallHierarchyOutgoingCall struct {
	To         CallHierarchyItem `json:"to"`
	FromRanges []Range           `json:"fromRanges"`
}

type InlineValueParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
	Context      struct {
		StoppedLocation Range `json:"stoppedLocation"`
	} `json:"context"`
}

type InlineValueVariableLookup struct {
	Range               Range  `json:"range"`
	VariableName        string `json:"variableName"`
	CaseSensitiveLookup bool   `json:"caseSensitiveLookup"`
}

type Moniker struct {
	Scheme     string `json:"scheme"`
	Identifier string `json:"identifier"`
	Unique     string `json:"unique"`
	Kind       string `json:"kind"`
}

type LinkedEditingRanges struct {
	Ranges      []Range `json:"ranges"`
	WordPattern string  `json:"wordPattern,omitempty"`
}

type WorkspaceFoldersChangeEvent struct {
	Added   []WorkspaceFolder `json:"added"`
	Removed []WorkspaceFolder `json:"removed"`
}

type DidChangeWorkspaceFoldersParams struct {
	Event WorkspaceFoldersChangeEvent `json:"event"`
}

type FileRename struct {
	OldURI string `json:"oldUri"`
	NewURI string `json:"newUri"`
}

type RenameFilesParams struct {
	Files []FileRename `json:"files"`
}

type FileOperationTarget struct {
	URI string `json:"uri"`
}

type FileOperationsParams struct {
	Files []FileOperationTarget `json:"files"`
}

type DocumentRangesFormattingParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Ranges       []Range                `json:"ranges"`
	Options      FormattingOptions      `json:"options"`
}
