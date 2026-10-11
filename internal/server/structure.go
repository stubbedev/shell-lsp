package server

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/structure"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

const (
	maxWorkspaceSymbols = 500
	progressStep        = 50
)

var symbolKinds = map[structure.SymbolKind]lsp.SymbolKind{
	structure.SymbolFile:     lsp.SymbolFile,
	structure.SymbolFunction: lsp.SymbolFunction,
	structure.SymbolVariable: lsp.SymbolVariable,
	structure.SymbolArray:    lsp.SymbolArray,
	structure.SymbolConstant: lsp.SymbolConstant,
	structure.SymbolHeredoc:  lsp.SymbolString,
	structure.SymbolTest:     lsp.SymbolMethod,
}

var foldKinds = map[structure.FoldKind]string{
	structure.FoldRegion:  lsp.FoldingRegion,
	structure.FoldComment: lsp.FoldingComment,
	structure.FoldImports: lsp.FoldingImports,
}

func (s *Server) handleDocumentSymbol(_ context.Context, docs *docstore.View, params json.RawMessage) (any, error) {
	symbolParams, err := lsp.DecodeParams[lsp.DocumentSymbolParams](params)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing textDocument/documentSymbol params: %v", err)
	}
	snap, err := s.snapshot(docs, symbolParams.TextDocument.URI)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%v", err)
	}
	symbols := workspace.Memo(snap.file, "structure.symbols", 0, func() []structure.Symbol { return structure.Symbols(snap.tree) })
	if s.clientCapabilities().TextDocument.DocumentSymbol.HierarchicalDocumentSymbolSupport {
		return documentSymbols(snap.doc, snap.encoding, symbols), nil
	}
	return appendFlatSymbols([]lsp.SymbolInformation{}, snap.doc, snap.encoding, symbols, ""), nil
}

func documentSymbols(doc *docstore.Document, enc docstore.Encoding, symbols []structure.Symbol) []lsp.DocumentSymbol {
	out := make([]lsp.DocumentSymbol, len(symbols))
	for i, sym := range symbols {
		out[i] = lsp.DocumentSymbol{
			Name:           sym.Name,
			Detail:         sym.Detail,
			Kind:           symbolKinds[sym.Kind],
			Range:          doc.RangeFor(sym.Span.Start, sym.Span.End, enc),
			SelectionRange: doc.RangeFor(sym.Selection.Start, sym.Selection.End, enc),
			Children:       documentSymbols(doc, enc, sym.Children),
		}
	}
	return out
}

func appendFlatSymbols(out []lsp.SymbolInformation, doc *docstore.Document, enc docstore.Encoding, symbols []structure.Symbol, container string) []lsp.SymbolInformation {
	for _, sym := range symbols {
		out = append(out, lsp.SymbolInformation{
			Name:          sym.Name,
			Kind:          symbolKinds[sym.Kind],
			Location:      lsp.Location{URI: doc.URI, Range: doc.RangeFor(sym.Span.Start, sym.Span.End, enc)},
			ContainerName: container,
		})
		out = appendFlatSymbols(out, doc, enc, sym.Children, sym.Name)
	}
	return out
}

func (s *Server) handleFoldingRange(_ context.Context, docs *docstore.View, params json.RawMessage) (any, error) {
	foldParams, err := lsp.DecodeParams[lsp.FoldingRangeParams](params)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing textDocument/foldingRange params: %v", err)
	}
	snap, err := s.snapshot(docs, foldParams.TextDocument.URI)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%v", err)
	}
	lineOf := func(offset int) int { return int(snap.doc.OffsetToPosition(offset, snap.encoding).Line) }
	folds := structure.Folds(snap.tree, lineOf)
	if limit := s.clientCapabilities().TextDocument.FoldingRange.RangeLimit; limit > 0 && len(folds) > limit {
		folds = folds[:limit]
	}
	out := make([]lsp.FoldingRange, len(folds))
	for i, f := range folds {
		out[i] = lsp.FoldingRange{StartLine: uint32(f.StartLine), EndLine: uint32(f.EndLine), Kind: foldKinds[f.Kind]} //nolint:gosec
	}
	return out, nil
}

func (s *Server) handleWorkspaceSymbol(ctx context.Context, docs *docstore.View, params json.RawMessage) (any, error) {
	symbolParams, err := lsp.DecodeParams[lsp.WorkspaceSymbolParams](params)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing workspace/symbol params: %v", err)
	}
	files, err := s.indexWorkspace(ctx, docs, interactive)
	if err != nil {
		return nil, err
	}
	out := []lsp.SymbolInformation{}
	for _, file := range files {
		for _, fn := range file.Tree.Functions {
			if structure.Matches(symbolParams.Query, fn.Name) {
				out = append(out, workspaceSymbol(file, docs.Encoding(), fn.Name, lsp.SymbolFunction, fn.Span))
			}
		}
		for _, def := range file.Tree.Definitions {
			if def.Within == parser.FileScope && def.Kind != parser.DefCommandEnv && structure.Matches(symbolParams.Query, def.Name) {
				out = append(out, workspaceSymbol(file, docs.Encoding(), def.Name, lsp.SymbolVariable, def.Span))
			}
		}
		for _, test := range file.Tree.Tests {
			if structure.Matches(symbolParams.Query, test.Name) {
				out = append(out, workspaceSymbol(file, docs.Encoding(), test.Name, symbolKinds[structure.SymbolTest], test.Span))
			}
		}
	}
	slices.SortStableFunc(out, func(a, b lsp.SymbolInformation) int {
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Name, b.Name), cmp.Compare(a.Location.URI, b.Location.URI))
	})
	out = slices.CompactFunc(out, func(a, b lsp.SymbolInformation) bool {
		return a.Kind == b.Kind && a.Name == b.Name && a.Location.URI == b.Location.URI
	})
	return out[:min(len(out), maxWorkspaceSymbols)], nil
}

func workspaceSymbol(file *workspace.File, enc docstore.Encoding, name string, kind lsp.SymbolKind, span parser.Span) lsp.SymbolInformation {
	return lsp.SymbolInformation{Name: name, Kind: kind, Location: lsp.Location{URI: file.URI, Range: file.Doc.RangeFor(span.Start, span.End, enc)}}
}

func (s *Server) indexWorkspace(ctx context.Context, docs *docstore.View, u urgency) ([]*workspace.File, error) {
	progress, ctx, err := s.beginWorkDone(ctx, "Indexing shell files")
	if err != nil {
		return nil, err
	}
	defer progress.end("done")
	return s.workspaceFiles(ctx, docs, u, func(done, total int) {
		if done%progressStep == 0 {
			progress.report(fmt.Sprintf("%d/%d files", done, total), done*100/max(total, 1))
		}
	})
}

func (s *Server) workspaceFiles(ctx context.Context, docs *docstore.View, u urgency, report func(done, total int)) ([]*workspace.File, error) {
	paths, err := s.workspace.ShellFiles(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var files []*workspace.File
	for _, doc := range docs.Documents() {
		file, err := s.workspaceFile(docs, doc.URI)
		if err != nil {
			return nil, err
		}
		seen[file.URI] = true
		files = append(files, file)
	}
	loaded, err := s.workspace.LoadAll(ctx, paths, u.workers(), report)
	if err != nil {
		return nil, err
	}
	for _, file := range loaded {
		if seen[file.URI] {
			continue
		}
		seen[file.URI] = true
		files = append(files, file)
	}
	return files, nil
}
