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

type DidSaveTextDocumentParams struct {
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
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}
