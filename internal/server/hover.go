package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/config"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/embedded"
	"github.com/stubbedev/shell-lsp/internal/hover"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/manpage"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/platform"
	"github.com/stubbedev/shell-lsp/internal/workspace"
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

func (s *Server) handleHover(ctx context.Context, docs *docstore.View, hoverParams lsp.HoverParams) (any, error) {
	here, inside, err := s.at(docs, hoverParams.TextDocumentPositionParams)
	if err != nil {
		return nil, err
	}
	if native, ok := nativeLocation(here, hoverParams.Position, inside); ok {
		if hover, handled, err := s.nativeHoverAt(ctx, docs, native); handled {
			return hover, err
		}
	}
	if !inside {
		return nil, nil
	}
	result, ok, err := s.shellHover(ctx, here.File, here.File, here.offset, here.settings, here.generation)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return s.hoverResult(here.snapshot, result.Markdown, result.Span), nil
}

func (s *Server) shellHover(ctx context.Context, whole, variant *workspace.File, offset int, settings config.Settings, generation uint64) (hover.Result, bool, error) {
	if h, ok := embedded.HoverAt(embeddedPrograms(variant), offset); ok {
		return hover.Result{Markdown: h.Markdown, Span: h.Span}, true, nil
	}
	return hover.Hover(ctx, hover.Input{
		Tree: variant.Tree, Imports: s.imports(variant), Offset: offset, Commands: s.commandDocs,
		Lint: s.lintOf(whole, settings, generation),
	})
}

func (s *Server) lintOf(file *workspace.File, settings config.Settings, generation uint64) func() (lint.Report, bool) {
	return func() (lint.Report, bool) {
		report, _ := s.lintReport(file, settings, generation)
		return report, settings.Lint.Enabled
	}
}

func (s *Server) hoverResult(snap *snapshot, markdown string, span parser.Span) lsp.Hover {
	r := snap.positions().Range(span)
	return lsp.Hover{Contents: lsp.MarkupContent{Kind: s.hoverFormat(), Value: markdown}, Range: &r}
}

func (s *Server) hoverFormat() lsp.MarkupKind {
	formats := s.clientCapabilities().TextDocument.Hover.ContentFormat
	if len(formats) == 0 || slices.Contains(formats, lsp.MarkupMarkdown) {
		return lsp.MarkupMarkdown
	}
	return lsp.MarkupPlainText
}
