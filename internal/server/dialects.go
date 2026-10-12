package server

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

const (
	documentDialectMethod = "shellLsp/documentDialect"
	setDialectMethod      = "shellLsp/setDialect"
)

var selectableDialects = []dialect.Dialect{dialect.Bash, dialect.Posix, dialect.Dash, dialect.Zsh, dialect.Ksh, dialect.Busybox}

var selectableNames = func() []string {
	names := make([]string, len(selectableDialects))
	for i, choice := range selectableDialects {
		names[i] = choice.String()
	}
	return names
}()

type documentDialectParams struct {
	TextDocument lsp.TextDocumentIdentifier `json:"textDocument"`
}

type detectedDialect struct {
	Dialect     string `json:"dialect"`
	Source      string `json:"source"`
	Evidence    string `json:"evidence,omitempty"`
	Guessed     bool   `json:"guessed"`
	Description string `json:"description"`
}

type directiveChoice struct {
	Directive bool     `json:"directive"`
	Dialects  []string `json:"dialects"`
}

type documentDialect struct {
	detected detectedDialect
	choice   *directiveChoice
}

func (d documentDialect) MarshalJSON() ([]byte, error) {
	if d.choice == nil {
		return json.Marshal(struct {
			Selectable bool `json:"selectable"`
			detectedDialect
		}{false, d.detected})
	}
	return json.Marshal(struct {
		Selectable bool `json:"selectable"`
		detectedDialect
		directiveChoice
	}{true, d.detected, *d.choice})
}

type setDialectParams struct {
	TextDocument lsp.TextDocumentIdentifier `json:"textDocument"`
	Dialect      *string                    `json:"dialect"`
}

func (s *Server) handleDocumentDialect(_ context.Context, docs *docstore.View, params json.RawMessage) (any, error) {
	p, err := lsp.DecodeParams[documentDialectParams](params)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing %s params: %v", documentDialectMethod, err)
	}
	snap, err := s.snapshot(docs, p.TextDocument.URI)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%v", err)
	}
	d := snap.detection
	answer := documentDialect{detected: detectedDialect{
		Dialect:     d.Dialect.String(),
		Source:      d.Source.String(),
		Evidence:    d.Evidence,
		Guessed:     d.Guessed(),
		Description: d.Describe(),
	}}
	if !snap.doc.Projected() {
		answer.choice = &directiveChoice{Directive: dialect.HasShellDirective(snap.doc.Text()), Dialects: selectableNames}
	}
	return answer, nil
}

func (s *Server) handleSetDialect(_ context.Context, docs *docstore.View, params json.RawMessage) (any, error) {
	p, err := lsp.DecodeParams[setDialectParams](params)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing %s params: %v", setDialectMethod, err)
	}
	var shell *dialect.Dialect
	if p.Dialect != nil {
		d, ok := dialect.FromName(*p.Dialect)
		if !ok || !slices.Contains(selectableDialects, d) {
			return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%s: unknown dialect %q", setDialectMethod, *p.Dialect)
		}
		shell = &d
	}
	snap, err := s.snapshot(docs, p.TextDocument.URI)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%v", err)
	}
	if snap.doc.Projected() {
		return nil, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "%s: the shell of %s is %s, so a directive cannot choose it", setDialectMethod, p.TextDocument.URI, snap.detection.Describe())
	}
	edits := dialect.ShellDirectiveEdits(snap.doc.Text(), shell)
	if len(edits) == 0 {
		return nil, nil
	}
	return s.workspaceEdit(snap.doc, snap.encoding, edits)
}
