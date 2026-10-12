package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/embedded"
	"github.com/stubbedev/shell-lsp/internal/hover"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/manpage"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/platform"
)

type commandDocs struct {
	*manpage.Lookup
	path    []string
	pathExt []string
}

func (c commandDocs) Describe(ctx context.Context, name string) (string, error) {
	page, err := c.Page(ctx, name)
	if err == nil {
		return page.Markdown(), nil
	}
	if !errors.Is(err, manpage.ErrNoPage) {
		return "", err
	}
	path, found := platform.Native.LookPath(name, c.path, c.pathExt, os.Stat)
	if !found {
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
	offset, inside := snap.Doc.Locate(hoverParams.Position, snap.encoding)
	if !inside {
		return nil, nil
	}
	if h, ok := embedded.HoverAt(embeddedPrograms(snap.File), offset); ok {
		return s.hoverResult(snap, h.Markdown, h.Span), nil
	}
	result, ok, err := hover.Hover(ctx, hover.Input{
		Tree: snap.Tree, Imports: s.imports(snap.File), Offset: offset, Commands: s.commandDocs,
		Lint: func() (lint.Report, bool) {
			report, _ := s.lintReport(snap.File, snap.settings, snap.generation)
			return report, snap.settings.Lint.Enabled
		},
	})
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return s.hoverResult(snap, result.Markdown, result.Span), nil
}

func (s *Server) hoverResult(snap *snapshot, markdown string, span parser.Span) lsp.Hover {
	r := snap.Doc.RangeFor(span.Start, span.End, snap.encoding)
	return lsp.Hover{Contents: lsp.MarkupContent{Kind: s.hoverFormat(), Value: markdown}, Range: &r}
}

func (s *Server) hoverFormat() string {
	formats := s.clientCapabilities().TextDocument.Hover.ContentFormat
	if len(formats) == 0 || slices.Contains(formats, lsp.MarkupMarkdown) {
		return lsp.MarkupMarkdown
	}
	return lsp.MarkupPlainText
}
