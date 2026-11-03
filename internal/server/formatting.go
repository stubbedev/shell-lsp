package server

import (
	"context"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/editorconfig"
	"github.com/stubbedev/shell-lsp/internal/format"
	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func (s *Server) handleFormatting(_ context.Context, docs *docstore.View, formatParams lsp.DocumentFormattingParams) (any, error) {
	return s.format(docs, formatParams.TextDocument.URI, formatParams.Options, nil)
}

func (s *Server) handleRangeFormatting(_ context.Context, docs *docstore.View, formatParams lsp.DocumentRangeFormattingParams) (any, error) {
	return s.format(docs, formatParams.TextDocument.URI, formatParams.Options, []lsp.Range{formatParams.Range})
}

func (s *Server) handleRangesFormatting(_ context.Context, docs *docstore.View, formatParams lsp.DocumentRangesFormattingParams) (any, error) {
	if len(formatParams.Ranges) == 0 {
		return []lsp.TextEdit{}, nil
	}
	return s.format(docs, formatParams.TextDocument.URI, formatParams.Options, formatParams.Ranges)
}

func (s *Server) handleOnTypeFormatting(_ context.Context, docs *docstore.View, typed lsp.DocumentOnTypeFormattingParams) (any, error) {
	snap, err := s.snapshot(docs, typed.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	if snap.Native == nil || snap.Native.Language != host.Nix || !snap.settings.Format.Enabled || snap.project.Ignore {
		return []lsp.TextEdit{}, nil
	}
	doc := snap.Native.Nix()
	at := snap.Doc.Source().Positions(snap.encoding)
	span, ok := doc.BindingEndingAt(snap.Doc.Source().PositionToOffset(typed.Position, snap.encoding))
	if !ok || doc.Broken() {
		return []lsp.TextEdit{}, nil
	}
	return s.formatNative(snap, []lsp.Range{at.Range(span)})
}

func (s *Server) format(docs *docstore.View, uri string, opts lsp.FormattingOptions, within []lsp.Range) ([]lsp.TextEdit, error) {
	snap, err := s.snapshot(docs, uri)
	if err != nil {
		return nil, err
	}
	if !snap.settings.Format.Enabled {
		return nil, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "formatting is disabled by shellLsp.format.enable")
	}
	if snap.projectErr != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "%v", snap.projectErr)
	}
	if snap.project.Ignore {
		s.noticeOnce("editorconfig-ignore:"+uri, lsp.MessageInfo, "Not formatting "+snap.Path+": "+editorconfig.FileName+" marks it ignore = true.")
		return []lsp.TextEdit{}, nil
	}
	if snap.Native != nil && snap.Native.Formats() {
		return s.formatNative(snap, within)
	}
	if snap.Doc.Projected() {
		s.noticeOnce("format-projected:"+uri, lsp.MessageInfo, "Not formatting "+uri+": shell-lsp formats shell scripts, justfiles, Makefiles and Nix files, not the shell embedded in other files.")
		return []lsp.TextEdit{}, nil
	}
	text := snap.Doc.Text()
	formatted, err := format.Format(snap.Tree, snap.formatStyle().Over(clientStyle(opts)).Options())
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "%v", err)
	}
	at := snap.positions()
	edits := format.Edits(text, formatted)
	if within != nil {
		edits = format.RangeEdits(text, edits, selectedSpans(at, within), nil)
	}
	out, ok := at.TextEdits(edits)
	if !ok {
		panic("server: formatting edits " + uri + ", which is not projected, yet its edits could not be written")
	}
	return out, nil
}

func selectedSpans(at docstore.Positions, within []lsp.Range) []parser.Span {
	spans := make([]parser.Span, len(within))
	for i, r := range within {
		spans[i], _ = at.Span(r)
	}
	return spans
}

func clientStyle(client lsp.FormattingOptions) format.Style {
	indent := uint(0)
	if client.InsertSpaces {
		indent = client.TabSize
	}
	return format.Style{Indent: &indent}
}

func (s *Server) formatNative(snap *snapshot, within []lsp.Range) ([]lsp.TextEdit, error) {
	formatted, err := snap.Native.Format()
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "%v", err)
	}
	source := snap.Doc.Source()
	at := source.Positions(snap.encoding)
	edits := format.Edits(source.Text(), formatted)
	if within != nil {
		edits = format.RangeEdits(source.Text(), edits, selectedSpans(at, within), snap.Native.MoveTogether())
	}
	out, ok := at.TextEdits(edits)
	if !ok {
		panic("server: formatting edits for the unprojected source of " + source.URI + " could not be written")
	}
	return out, nil
}
