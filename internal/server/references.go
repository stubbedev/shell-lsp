package server

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

func (s *Server) positionFile(docs *docstore.View, params lsp.TextDocumentPositionParams) (*workspace.File, int, error) {
	file, err := s.workspaceFile(docs, params.TextDocument.URI)
	if err != nil {
		return nil, 0, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%v", err)
	}
	return file, file.Doc.PositionToOffset(params.Position, docs.Encoding()), nil
}

func (s *Server) handleReferences(ctx context.Context, docs *docstore.View, params json.RawMessage) (any, error) {
	refParams, err := lsp.DecodeParams[lsp.ReferenceParams](params)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing textDocument/references params: %v", err)
	}
	file, offset, err := s.positionFile(docs, refParams.TextDocumentPositionParams)
	if err != nil {
		return nil, err
	}
	occs, err := navigation.References(ctx, file, offset, s.navigationEnv(), s.workspace.ShellFiles, refParams.Context.IncludeDeclaration)
	if err != nil {
		return nil, err
	}
	out := make([]lsp.Location, len(occs))
	for i, o := range occs {
		out[i] = lsp.Location{URI: o.File.URI, Range: o.File.Doc.RangeFor(o.Span.Start, o.Span.End, docs.Encoding())}
	}
	return out, nil
}

func (s *Server) handleDocumentHighlight(ctx context.Context, docs *docstore.View, params json.RawMessage) (any, error) {
	position, err := lsp.DecodeParams[lsp.TextDocumentPositionParams](params)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing textDocument/documentHighlight params: %v", err)
	}
	file, offset, err := s.positionFile(docs, position)
	if err != nil {
		return nil, err
	}
	sym, ok := navigation.SymbolAt(file, offset)
	if !ok {
		return []lsp.DocumentHighlight{}, nil
	}
	occs := navigation.Occurrences(sym, []*workspace.File{file})
	out := make([]lsp.DocumentHighlight, len(occs))
	for i, o := range occs {
		kind := lsp.HighlightRead
		if o.Write {
			kind = lsp.HighlightWrite
		}
		out[i] = lsp.DocumentHighlight{Range: file.Doc.RangeFor(o.Span.Start, o.Span.End, docs.Encoding()), Kind: kind}
	}
	return out, nil
}

func (s *Server) handlePrepareRename(ctx context.Context, docs *docstore.View, params json.RawMessage) (any, error) {
	position, err := lsp.DecodeParams[lsp.TextDocumentPositionParams](params)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing textDocument/prepareRename params: %v", err)
	}
	file, offset, err := s.positionFile(docs, position)
	if err != nil {
		return nil, err
	}
	sym, _, err := navigation.PrepareRename(ctx, file, offset, s.navigationEnv(), s.workspace.ShellFiles)
	if errors.Is(err, navigation.ErrNoSymbol) {
		return nil, nil
	}
	if err != nil {
		return nil, renameError(err)
	}
	return lsp.PrepareRenameResult{Range: file.Doc.RangeFor(sym.Span.Start, sym.Span.End, docs.Encoding()), Placeholder: sym.Name}, nil
}

func (s *Server) handleRename(ctx context.Context, docs *docstore.View, params json.RawMessage) (any, error) {
	renameParams, err := lsp.DecodeParams[lsp.RenameParams](params)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing textDocument/rename params: %v", err)
	}
	file, offset, err := s.positionFile(docs, renameParams.TextDocumentPositionParams)
	if err != nil {
		return nil, err
	}
	occs, err := navigation.Rename(ctx, file, offset, renameParams.NewName, s.navigationEnv(), s.workspace.ShellFiles)
	if err != nil {
		return nil, renameError(err)
	}
	var files []fileEdits
	index := map[string]int{}
	for _, o := range occs {
		i, ok := index[o.File.URI]
		if !ok {
			_, open := docs.Document(o.File.URI)
			i = len(files)
			index[o.File.URI] = i
			files = append(files, fileEdits{doc: o.File.Doc, versioned: open})
		}
		files[i].edits = append(files[i].edits, textedit.Edit{Start: o.Span.Start, End: o.Span.End, NewText: renameParams.NewName})
	}
	return s.multiFileEdit(docs.Encoding(), files), nil
}

func renameError(err error) error {
	if errors.Is(err, navigation.ErrInvalidName) || errors.Is(err, navigation.ErrNotRenamable) || errors.Is(err, navigation.ErrNameConflict) || errors.Is(err, navigation.ErrNoSymbol) {
		return jsonrpc.NewError(jsonrpc.CodeRequestFailed, "%v", err)
	}
	return err
}
