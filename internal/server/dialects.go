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
	Position     *lsp.Position              `json:"position,omitempty"`
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

func (s *Server) handleDocumentDialect(_ context.Context, docs *docstore.View, p documentDialectParams) (any, error) {
	snap, err := s.snapshot(docs, p.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	if p.Position != nil {
		if offset, inside := snap.positions().Offset(*p.Position); inside {
			snap = snap.at(offset)
		}
	}
	d := snap.Detection
	answer := documentDialect{detected: detectedDialect{
		Dialect:     d.Dialect.String(),
		Source:      d.Source.String(),
		Evidence:    d.Evidence,
		Guessed:     d.Guessed(),
		Description: d.Describe(),
	}}
	if !snap.Doc.Projected() {
		answer.choice = &directiveChoice{Directive: dialect.HasShellDirective(snap.Doc.Text()), Dialects: selectableNames}
	}
	return answer, nil
}

func (s *Server) handleSetDialect(_ context.Context, docs *docstore.View, p setDialectParams) (any, error) {
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
		return nil, err
	}
	if snap.Doc.Projected() {
		return nil, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "%s: the shell of %s is %s, so a directive cannot choose it", setDialectMethod, p.TextDocument.URI, snap.Detection.Describe())
	}
	edits := dialect.ShellDirectiveEdits(snap.Doc.Text(), shell)
	if len(edits) == 0 {
		return nil, nil
	}
	return s.workspaceEdit(snap.Doc, snap.encoding, edits)
}
