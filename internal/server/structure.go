package server

import (
	"cmp"
	"context"
	"fmt"
	"iter"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/structure"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

const (
	maxWorkspaceSymbols = 500
	progressStep        = 50
)

func symbolKind(kind structure.SymbolKind) lsp.SymbolKind {
	switch kind {
	case structure.SymbolFile:
		return lsp.SymbolFile
	case structure.SymbolFunction:
		return lsp.SymbolFunction
	case structure.SymbolVariable:
		return lsp.SymbolVariable
	case structure.SymbolArray:
		return lsp.SymbolArray
	case structure.SymbolConstant:
		return lsp.SymbolConstant
	case structure.SymbolHeredoc:
		return lsp.SymbolString
	case structure.SymbolTest:
		return lsp.SymbolMethod
	}
	panic(fmt.Sprintf("server: symbol kind %d has no LSP kind", kind))
}

func foldKind(kind structure.FoldKind) lsp.FoldingRangeKind {
	switch kind {
	case structure.FoldRegion:
		return lsp.FoldingRegion
	case structure.FoldComment:
		return lsp.FoldingComment
	case structure.FoldImports:
		return lsp.FoldingImports
	case structure.FoldBlock:
		return ""
	}
	panic(fmt.Sprintf("server: fold kind %d has no LSP kind", kind))
}

func (s *Server) handleDocumentSymbol(_ context.Context, docs *docstore.View, symbolParams lsp.DocumentSymbolParams) (any, error) {
	snap, err := s.snapshot(docs, symbolParams.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	symbols := workspace.Memo(snap.File, "structure.symbols", 0, func() []structure.Symbol { return structure.Symbols(snap.Tree) })
	if snap.Native != nil {
		tree := nestShellSymbols(s.nativeSymbols(snap), documentSymbols(snap.positions(), authoredSymbols(snap.Doc, symbols)))
		if s.clientCapabilities().TextDocument.DocumentSymbol.HierarchicalDocumentSymbolSupport {
			return tree, nil
		}
		return flatten([]lsp.SymbolInformation{}, snap.URI, tree, ""), nil
	}
	if s.clientCapabilities().TextDocument.DocumentSymbol.HierarchicalDocumentSymbolSupport {
		return documentSymbols(snap.positions(), symbols), nil
	}
	return appendFlatSymbols([]lsp.SymbolInformation{}, snap.URI, snap.positions(), symbols, ""), nil
}

func authoredSymbols(doc *docstore.Document, symbols []structure.Symbol) []structure.Symbol {
	out := make([]structure.Symbol, 0, len(symbols))
	for _, sym := range symbols {
		if !doc.Synthetic(sym.Selection.Start) {
			sym.Children = authoredSymbols(doc, sym.Children)
			out = append(out, sym)
		}
	}
	return out
}

func within(inner, outer lsp.Range) bool {
	before := func(a, b lsp.Position) bool { return a.Line < b.Line || a.Line == b.Line && a.Character <= b.Character }
	return before(outer.Start, inner.Start) && before(inner.End, outer.End)
}

func nestShellSymbols(native, shell []lsp.DocumentSymbol) []lsp.DocumentSymbol {
	for _, sym := range shell {
		i := slices.IndexFunc(native, func(n lsp.DocumentSymbol) bool { return within(sym.Range, n.Range) })
		if i < 0 {
			native = append(native, sym)
			continue
		}
		native[i].Children = append(native[i].Children, sym)
	}
	slices.SortStableFunc(native, func(a, b lsp.DocumentSymbol) int {
		return cmp.Or(cmp.Compare(a.Range.Start.Line, b.Range.Start.Line), cmp.Compare(a.Range.Start.Character, b.Range.Start.Character))
	})
	return native
}

func documentSymbols(at docstore.Positions, symbols []structure.Symbol) []lsp.DocumentSymbol {
	out := make([]lsp.DocumentSymbol, len(symbols))
	for i, sym := range symbols {
		out[i] = lsp.DocumentSymbol{
			Name:           sym.Name,
			Detail:         sym.Detail,
			Kind:           symbolKind(sym.Kind),
			Range:          at.Range(sym.Span),
			SelectionRange: at.Range(sym.Selection),
			Children:       documentSymbols(at, sym.Children),
		}
	}
	return out
}

func appendFlatSymbols(out []lsp.SymbolInformation, uri string, at docstore.Positions, symbols []structure.Symbol, container string) []lsp.SymbolInformation {
	for _, sym := range symbols {
		out = append(out, lsp.SymbolInformation{
			Name:          sym.Name,
			Kind:          symbolKind(sym.Kind),
			Location:      lsp.Location{URI: uri, Range: at.Range(sym.Span)},
			ContainerName: container,
		})
		out = appendFlatSymbols(out, uri, at, sym.Children, sym.Name)
	}
	return out
}

func (s *Server) handleFoldingRange(_ context.Context, docs *docstore.View, foldParams lsp.FoldingRangeParams) (any, error) {
	snap, err := s.snapshot(docs, foldParams.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	lineOf := func(offset int) int { return int(snap.positions().Position(offset).Line) }
	folds := foldRanges(structure.Folds(snap.Tree, lineOf))
	if snap.Native != nil {
		folds = mergeFolds(nativeFolds(snap), folds)
	}
	return limitFolds(folds, s.clientCapabilities().TextDocument.FoldingRange.RangeLimit), nil
}

func foldRanges(folds []structure.Fold) []lsp.FoldingRange {
	out := make([]lsp.FoldingRange, len(folds))
	for i, f := range folds {
		out[i] = lsp.FoldingRange{StartLine: uint32(f.StartLine), EndLine: uint32(f.EndLine), Kind: foldKind(f.Kind)} //nolint:gosec
	}
	return out
}

func mergeFolds(a, b []lsp.FoldingRange) []lsp.FoldingRange {
	out := slices.Concat(a, b)
	slices.SortStableFunc(out, func(x, y lsp.FoldingRange) int {
		return cmp.Or(cmp.Compare(x.StartLine, y.StartLine), cmp.Compare(y.EndLine, x.EndLine))
	})
	return slices.CompactFunc(out, func(x, y lsp.FoldingRange) bool { return x.StartLine == y.StartLine })
}

func limitFolds(folds []lsp.FoldingRange, limit int) []lsp.FoldingRange {
	if limit > 0 && len(folds) > limit {
		return folds[:limit]
	}
	return append([]lsp.FoldingRange{}, folds...)
}

func (s *Server) handleWorkspaceSymbol(ctx context.Context, docs *docstore.View, symbolParams lsp.WorkspaceSymbolParams) (any, error) {
	files, err := s.indexWorkspace(ctx, docs, interactive)
	if err != nil {
		return nil, err
	}
	out := []lsp.SymbolInformation{}
	for _, file := range files {
		for _, fn := range file.Tree.Functions {
			if structure.Matches(symbolParams.Query, fn.Name) {
				out = append(out, workspaceSymbol(file, docs, fn.Name, lsp.SymbolFunction, fn.Span))
			}
		}
		for def := range globalDefinitions(file.Tree) {
			if structure.Matches(symbolParams.Query, def.Name) && !file.Doc.Synthetic(def.NameSpan.Start) {
				out = append(out, workspaceSymbol(file, docs, def.Name, lsp.SymbolVariable, def.Span))
			}
		}
		for _, test := range file.Tree.Tests {
			if structure.Matches(symbolParams.Query, test.Name) {
				out = append(out, workspaceSymbol(file, docs, test.Name, symbolKind(structure.SymbolTest), test.Span))
			}
		}
		if file.Native == nil {
			continue
		}
		at := sourcePositions(file, docs.Encoding())
		for _, sym := range file.Native.Index.Symbols {
			if sym.Scope == model.Global && structure.Matches(symbolParams.Query, sym.Name) {
				out = append(out, lsp.SymbolInformation{Name: sym.Name, Kind: nativeKind(sym.Kind), Location: lsp.Location{URI: file.URI, Range: at.Range(sym.Selection)}})
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

func globalDefinitions(tree *parser.Tree) iter.Seq[parser.Definition] {
	return func(yield func(parser.Definition) bool) {
		seen := map[string]bool{}
		for _, def := range tree.Definitions {
			if def.Within != parser.FileScope || def.Kind == parser.DefCommandEnv || seen[def.Name] {
				continue
			}
			seen[def.Name] = true
			if !yield(def) {
				return
			}
		}
	}
}

func workspaceSymbol(file *workspace.File, docs *docstore.View, name string, kind lsp.SymbolKind, span parser.Span) lsp.SymbolInformation {
	return lsp.SymbolInformation{Name: name, Kind: kind, Location: lsp.Location{URI: file.URI, Range: docs.Positions(file.Doc).Range(span)}}
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
