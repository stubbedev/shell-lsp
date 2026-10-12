package server

import (
	"context"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/editorconfig"
	"github.com/stubbedev/shell-lsp/internal/format"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/textedit"
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

func (s *Server) format(docs *docstore.View, uri string, opts lsp.FormattingOptions, within []lsp.Range) ([]lsp.TextEdit, error) {
	snap, err := s.snapshot(docs, uri)
	if err != nil {
		return nil, err
	}
	if !snap.settings.Format.Enabled {
		return nil, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "formatting is disabled by shellLsp.format.enable")
	}
	if snap.Doc.Projected() {
		s.noticeOnce("format-projected:"+uri, lsp.MessageInfo, "Not formatting "+uri+": shell-lsp formats shell scripts, not the shell embedded in other files.")
		return []lsp.TextEdit{}, nil
	}
	if snap.projectErr != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "%v", snap.projectErr)
	}
	if snap.project.Ignore {
		s.noticeOnce("editorconfig-ignore:"+uri, lsp.MessageInfo, "Not formatting "+snap.Path+": "+editorconfig.FileName+" marks it ignore = true.")
		return []lsp.TextEdit{}, nil
	}
	text := snap.Doc.Text()
	formatted, err := format.Format(snap.Tree, snap.formatStyle().Over(clientStyle(opts)).Options())
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "%v", err)
	}
	edits := format.Edits(text, formatted)
	if within != nil {
		var kept []textedit.Edit
		for _, e := range edits {
			if slices.ContainsFunc(within, func(r lsp.Range) bool {
				return len(format.RangeEdits([]textedit.Edit{e}, snap.Doc.PositionToOffset(r.Start, snap.encoding), snap.Doc.PositionToOffset(r.End, snap.encoding))) > 0
			}) {
				kept = append(kept, e)
			}
		}
		edits = kept
	}
	out, _ := snap.Doc.TextEdits(edits, snap.encoding)
	return out, nil
}

func clientStyle(client lsp.FormattingOptions) format.Style {
	indent := uint(0)
	if client.InsertSpaces {
		indent = client.TabSize
	}
	return format.Style{Indent: &indent}
}
