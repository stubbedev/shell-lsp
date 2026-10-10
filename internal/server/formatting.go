package server

import (
	"context"
	"encoding/json"

	"github.com/stubbedev/shell-lsp/internal/config"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/format"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

func (s *Server) handleFormatting(_ context.Context, docs *docstore.View, params json.RawMessage) (any, error) {
	formatParams, err := lsp.DecodeParams[lsp.DocumentFormattingParams](params)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing textDocument/formatting params: %v", err)
	}
	return s.format(docs, formatParams.TextDocument.URI, formatParams.Options, nil)
}

func (s *Server) handleRangeFormatting(_ context.Context, docs *docstore.View, params json.RawMessage) (any, error) {
	formatParams, err := lsp.DecodeParams[lsp.DocumentRangeFormattingParams](params)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing textDocument/rangeFormatting params: %v", err)
	}
	return s.format(docs, formatParams.TextDocument.URI, formatParams.Options, &formatParams.Range)
}

func (s *Server) format(docs *docstore.View, uri string, opts lsp.FormattingOptions, within *lsp.Range) ([]lsp.TextEdit, error) {
	snap, err := s.snapshot(docs, uri)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%v", err)
	}
	if !snap.settings.Format.Enabled {
		return nil, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "formatting is disabled by shellLsp.format.enable")
	}
	text := snap.doc.Text()
	formatted, err := format.Format(text, snap.dialect, formatOptions(snap.settings.Format, opts))
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "%v", err)
	}
	edits := format.Edits(text, formatted)
	if within != nil {
		edits = format.RangeEdits(edits, snap.doc.PositionToOffset(within.Start, snap.encoding), snap.doc.PositionToOffset(within.End, snap.encoding))
	}
	out := make([]lsp.TextEdit, len(edits))
	for i, e := range edits {
		out[i] = lsp.TextEdit{Range: snap.doc.RangeFor(e.Start, e.End, snap.encoding), NewText: e.NewText}
	}
	return out, nil
}

func formatOptions(settings config.Format, client lsp.FormattingOptions) format.Options {
	indent := uint(0)
	if client.InsertSpaces {
		indent = client.TabSize
	}
	if settings.Indent != nil {
		indent = *settings.Indent
	}
	return format.Options{
		Indent:           indent,
		BinaryNextLine:   settings.BinaryNextLine,
		SwitchCaseIndent: settings.SwitchCaseIndent,
		SpaceRedirects:   settings.SpaceRedirects,
		FunctionNextLine: settings.FunctionNextLine,
		Simplify:         settings.Simplify,
		Minify:           settings.Minify,
	}
}
