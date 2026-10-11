package server

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/editorconfig"
	"github.com/stubbedev/shell-lsp/internal/format"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/textedit"
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
	return s.format(docs, formatParams.TextDocument.URI, formatParams.Options, []lsp.Range{formatParams.Range})
}

func (s *Server) handleRangesFormatting(_ context.Context, docs *docstore.View, params json.RawMessage) (any, error) {
	formatParams, err := lsp.DecodeParams[lsp.DocumentRangesFormattingParams](params)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing textDocument/rangesFormatting params: %v", err)
	}
	if len(formatParams.Ranges) == 0 {
		return []lsp.TextEdit{}, nil
	}
	return s.format(docs, formatParams.TextDocument.URI, formatParams.Options, formatParams.Ranges)
}

func (s *Server) format(docs *docstore.View, uri string, opts lsp.FormattingOptions, within []lsp.Range) ([]lsp.TextEdit, error) {
	snap, err := s.snapshot(docs, uri)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%v", err)
	}
	if !snap.settings.Format.Enabled {
		return nil, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "formatting is disabled by shellLsp.format.enable")
	}
	if snap.projectErr != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "%v", snap.projectErr)
	}
	if snap.project.ignore {
		s.noticeOnce("editorconfig-ignore:"+uri, lsp.MessageInfo, "Not formatting "+snap.file.Path+": "+editorconfig.FileName+" marks it ignore = true.")
		return []lsp.TextEdit{}, nil
	}
	text := snap.doc.Text()
	formatted, err := format.Format(snap.tree, snap.formatStyle().Over(clientStyle(opts)).Options())
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "%v", err)
	}
	edits := format.Edits(text, formatted)
	if within != nil {
		var kept []textedit.Edit
		for _, e := range edits {
			if slices.ContainsFunc(within, func(r lsp.Range) bool {
				return len(format.RangeEdits([]textedit.Edit{e}, snap.doc.PositionToOffset(r.Start, snap.encoding), snap.doc.PositionToOffset(r.End, snap.encoding))) > 0
			}) {
				kept = append(kept, e)
			}
		}
		edits = kept
	}
	out := make([]lsp.TextEdit, len(edits))
	for i, e := range edits {
		out[i] = lsp.TextEdit{Range: snap.doc.RangeFor(e.Start, e.End, snap.encoding), NewText: e.NewText}
	}
	return out, nil
}

func clientStyle(client lsp.FormattingOptions) format.Style {
	indent := uint(0)
	if client.InsertSpaces {
		indent = client.TabSize
	}
	return format.Style{Indent: &indent}
}
