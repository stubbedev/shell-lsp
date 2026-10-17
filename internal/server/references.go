package server

import (
	"context"
	"errors"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/hostnav"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

func (s *Server) handleReferences(ctx context.Context, docs *docstore.View, refParams lsp.ReferenceParams) (any, error) {
	return s.referenceLocations(ctx, docs, refParams.TextDocumentPositionParams, refParams.Context.IncludeDeclaration)
}

func (s *Server) referenceLocations(ctx context.Context, docs *docstore.View, position lsp.TextDocumentPositionParams, includeDeclaration bool) ([]lsp.Location, error) {
	here, inside, err := s.at(docs, position)
	if err != nil {
		return nil, err
	}
	if native, ok := nativeLocation(here, position.Position, inside); ok {
		hit, err := s.nativeAt(ctx, docs, native)
		if err != nil {
			return nil, err
		}
		if hit.found || !inside {
			return hostOccurrences(hostnav.References(hit.group, hit.symbol, includeDeclaration), here.encoding), nil
		}
	}
	if !inside {
		return nil, nil
	}
	occs, err := navigation.References(ctx, here.File, here.offset, s.navigationEnv(), s.candidateFiles(docs), includeDeclaration)
	if err != nil {
		return nil, err
	}
	out := make([]lsp.Location, len(occs))
	for i, o := range occs {
		out[i] = lsp.Location{URI: o.File.URI, Range: docs.Positions(o.File.Doc).Range(o.Span)}
	}
	return out, nil
}

func (s *Server) handleDocumentHighlight(ctx context.Context, docs *docstore.View, position lsp.TextDocumentPositionParams) (any, error) {
	here, inside, err := s.at(docs, position)
	if err != nil {
		return nil, err
	}
	if native, ok := nativeLocation(here, position.Position, inside); ok {
		hit, err := s.nativeAt(ctx, docs, native)
		if err != nil {
			return nil, err
		}
		if hit.found || !inside {
			out := []lsp.DocumentHighlight{}
			for _, o := range hit.group.Highlights(hit.symbol) {
				kind := lsp.HighlightRead
				if o.Write {
					kind = lsp.HighlightWrite
				}
				out = append(out, lsp.DocumentHighlight{Range: sourcePositions(here.File, here.encoding).Range(o.Span), Kind: kind})
			}
			return out, nil
		}
	}
	if !inside {
		return nil, nil
	}
	sym, ok := navigation.SymbolAt(here.File, here.offset)
	if !ok {
		return []lsp.DocumentHighlight{}, nil
	}
	occs := navigation.Occurrences(sym, []*workspace.File{here.File})
	out := make([]lsp.DocumentHighlight, len(occs))
	for i, o := range occs {
		kind := lsp.HighlightRead
		if o.Write {
			kind = lsp.HighlightWrite
		}
		out[i] = lsp.DocumentHighlight{Range: here.positions().Range(o.Span), Kind: kind}
	}
	return out, nil
}

func (s *Server) handlePrepareRename(ctx context.Context, docs *docstore.View, position lsp.TextDocumentPositionParams) (any, error) {
	here, inside, err := s.at(docs, position)
	if err != nil {
		return nil, err
	}
	if native, ok := nativeLocation(here, position.Position, inside); ok {
		hit, err := s.nativeAt(ctx, docs, native)
		if err != nil {
			return nil, err
		}
		if hit.found || !inside {
			if !hit.found {
				return nil, nil
			}
			if err := hit.group.PrepareRename(hit.symbol); err != nil {
				return nil, renameError(err)
			}
			return lsp.PrepareRenameResult{Range: sourcePositions(here.File, here.encoding).Range(hit.symbol.Span), Placeholder: hit.symbol.Name}, nil
		}
	}
	if !inside {
		return nil, nil
	}
	sym, _, err := navigation.PrepareRename(ctx, here.File, here.offset, s.navigationEnv(), s.candidateFiles(docs))
	if errors.Is(err, navigation.ErrNoSymbol) {
		return nil, nil
	}
	if err != nil {
		return nil, renameError(err)
	}
	return lsp.PrepareRenameResult{Range: here.positions().Range(sym.Span), Placeholder: sym.Name}, nil
}

func (s *Server) handleRename(ctx context.Context, docs *docstore.View, renameParams lsp.RenameParams) (any, error) {
	here, inside, err := s.at(docs, renameParams.TextDocumentPositionParams)
	if err != nil {
		return nil, err
	}
	if native, ok := nativeLocation(here, renameParams.Position, inside); ok {
		hit, err := s.nativeAt(ctx, docs, native)
		if err != nil {
			return nil, err
		}
		if hit.found {
			return s.nativeRename(hit, docs.Encoding(), docs, renameParams.NewName)
		}
		if !inside {
			return nil, renameError(navigation.ErrNoSymbol)
		}
	}
	if !inside {
		return nil, nil
	}
	occs, err := navigation.Rename(ctx, here.File, here.offset, renameParams.NewName, s.navigationEnv(), s.candidateFiles(docs))
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
	return s.multiFileEdit(docs.Encoding(), files)
}

func renameError(err error) error {
	if errors.Is(err, navigation.ErrInvalidName) || errors.Is(err, navigation.ErrNotRenamable) || errors.Is(err, navigation.ErrNameConflict) || errors.Is(err, navigation.ErrNoSymbol) {
		return jsonrpc.NewError(jsonrpc.CodeRequestFailed, "%v", err)
	}
	return err
}
