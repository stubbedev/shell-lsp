package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/hover"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/manpage"
)

type commandDocs struct {
	pages *manpage.Lookup
}

func (c commandDocs) Describe(ctx context.Context, name string) (string, error) {
	page, err := c.pages.Page(ctx, name)
	if err == nil {
		return page.Markdown(), nil
	}
	if !errors.Is(err, manpage.ErrNoPage) {
		return "", err
	}
	path, lookErr := exec.LookPath(name)
	if lookErr != nil {
		return fmt.Sprintf("`%s`: command not found in `PATH`", name), nil
	}
	return fmt.Sprintf("`%s`: external command at `%s` (no manual page installed)", name, path), nil
}

func (s *Server) handleHover(ctx context.Context, docs *docstore.View, params json.RawMessage) (any, error) {
	hoverParams, err := lsp.DecodeParams[lsp.HoverParams](params)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing textDocument/hover params: %v", err)
	}
	snap, err := s.snapshot(docs, hoverParams.TextDocument.URI)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%v", err)
	}
	offset := snap.doc.PositionToOffset(hoverParams.Position, snap.encoding)
	result, ok, err := hover.Hover(ctx, hover.Input{Tree: snap.tree, Imports: s.imports(snap), Offset: offset, Commands: s.commandDocs})
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	r := snap.doc.RangeFor(result.Span.Start, result.Span.End, snap.encoding)
	return lsp.Hover{Contents: lsp.MarkupContent{Kind: s.hoverFormat(), Value: result.Markdown}, Range: &r}, nil
}

func (s *Server) hoverFormat() string {
	formats := s.clientCapabilities().TextDocument.Hover.ContentFormat
	if len(formats) == 0 || slices.Contains(formats, lsp.MarkupMarkdown) {
		return lsp.MarkupMarkdown
	}
	return lsp.MarkupPlainText
}
