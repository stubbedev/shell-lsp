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

type documentDialectParams struct {
	TextDocument lsp.TextDocumentIdentifier `json:"textDocument"`
}

type documentDialect struct {
	Dialect     string   `json:"dialect"`
	Source      string   `json:"source"`
	Evidence    string   `json:"evidence,omitempty"`
	Guessed     bool     `json:"guessed"`
	Description string   `json:"description"`
	Directive   bool     `json:"directive"`
	Dialects    []string `json:"dialects"`
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
	names := []string{}
	if !snap.doc.Projected() {
		names = make([]string, len(selectableDialects))
		for i, choice := range selectableDialects {
			names[i] = choice.String()
		}
	}
	return documentDialect{
		Dialect:     d.Dialect.String(),
		Source:      d.Source.String(),
		Evidence:    d.Evidence,
		Guessed:     d.Guessed(),
		Description: d.Describe(),
		Directive:   !snap.doc.Projected() && dialect.HasShellDirective(snap.doc.Text()),
		Dialects:    names,
	}, nil
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
