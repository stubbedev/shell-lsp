package server

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/structure"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

const (
	maxIndexedSymbols       = 200_000
	maxWorkspaceDiagnostics = 100_000
	maxListedRecipes        = 10_000
)

type symbolIndex struct {
	mu      sync.Mutex
	stamp   symbolStamp
	built   bool
	symbols []lsp.SymbolInformation
	closes  atomic.Uint64
	limit   int
}

type symbolStamp struct {
	generation, closes uint64
}

func sortSymbols(symbols []lsp.SymbolInformation) []lsp.SymbolInformation {
	slices.SortStableFunc(symbols, func(a, b lsp.SymbolInformation) int {
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Name, b.Name), cmp.Compare(a.Location.URI, b.Location.URI))
	})
	return slices.CompactFunc(symbols, func(a, b lsp.SymbolInformation) bool {
		return a.Kind == b.Kind && a.Name == b.Name && a.Location.URI == b.Location.URI
	})
}

func closedFiles(docs *docstore.View, wanted func(path string) bool) func(string) bool {
	return func(path string) bool {
		if !wanted(path) {
			return false
		}
		_, open := docs.Document(fileuri.FromPath(path))
		return !open
	}
}

func everyFile(string) bool { return true }

func (s *Server) eachDiskFile(ctx context.Context, wanted func(path string) bool, visit func(*workspace.File) bool) (int, int, error) {
	paths, err := s.workspace.ShellFiles(ctx)
	if err != nil {
		return 0, 0, err
	}
	paths = slices.DeleteFunc(paths, func(path string) bool { return !wanted(path) })
	progress, ctx, err := s.beginWorkDone(ctx, "Indexing shell files")
	if err != nil {
		return 0, 0, err
	}
	defer progress.end("done")
	visited, err := s.eachFile(ctx, paths, visit, func(visited int) {
		progress.report(fmt.Sprintf("%d/%d files", visited, len(paths)), visited*100/max(len(paths), 1))
	})
	return visited, len(paths), err
}

func (s *Server) eachFile(ctx context.Context, paths []string, visit func(*workspace.File) bool, batched func(visited int)) (int, error) {
	visited := 0
	for batch := range s.workspace.Batches(paths) {
		files, err := s.workspace.LoadAll(ctx, batch, interactive.workers(), func(int, int) {})
		if err != nil {
			return visited, err
		}
		for _, file := range files {
			visited++
			if !visit(file) {
				return visited, nil
			}
		}
		batched(visited)
	}
	return visited, nil
}

func (s *Server) noticeScanCut(what string) {
	if s.workspace.ScanCut() {
		s.noticeOnce("scan-cut:"+what, lsp.MessageWarning, what+" stopped at the file limit of one workspace scan, so files past it are not covered.")
	}
}

func (s *Server) noticeIndexLimit(what string, limit, visited, total int) {
	if visited < total {
		s.noticeOnce(fmt.Sprintf("index-limit:%s:%d:%d", what, visited, total), lsp.MessageWarning, fmt.Sprintf("%s reached its limit of %d after %d of %d files, so the rest are not covered.", what, limit, visited, total))
	}
}

func (s *Server) diskSymbols(ctx context.Context, docs *docstore.View) ([]lsp.SymbolInformation, error) {
	index := &s.symbols
	index.mu.Lock()
	defer index.mu.Unlock()
	stamp := symbolStamp{generation: s.workspace.Generation(), closes: index.closes.Load()}
	if index.built && index.stamp == stamp && s.workspace.Watched() {
		return index.symbols, nil
	}
	limit := cmp.Or(index.limit, maxIndexedSymbols)
	symbols := []lsp.SymbolInformation{}
	limited := false
	visited, total, err := s.eachDiskFile(ctx, everyFile, func(file *workspace.File) bool {
		for _, symbol := range fileSymbols(file, docs) {
			symbol.Name = strings.Clone(symbol.Name)
			symbols = append(symbols, symbol)
		}
		limited = len(symbols) >= limit
		return !limited
	})
	if err != nil {
		return nil, err
	}
	index.symbols, index.stamp, index.built = sortSymbols(symbols), stamp, true
	s.noticeScanCut("The workspace symbol index")
	s.noticeIndexLimit("The workspace symbol index", limit, visited, total)
	return index.symbols, nil
}

func (s *Server) matchingSymbols(ctx context.Context, docs *docstore.View, query string) ([]lsp.SymbolInformation, error) {
	disk, err := s.diskSymbols(ctx, docs)
	if err != nil {
		return nil, err
	}
	out := []lsp.SymbolInformation{}
	for _, doc := range docs.Documents() {
		file, err := s.workspaceFile(docs, doc.URI)
		if err != nil {
			return nil, err
		}
		for _, symbol := range fileSymbols(file, docs) {
			if structure.Matches(query, symbol.Name) {
				out = append(out, symbol)
			}
		}
	}
	found := 0
	for _, symbol := range disk {
		if found == maxWorkspaceSymbols {
			break
		}
		if !structure.Matches(query, symbol.Name) {
			continue
		}
		if _, open := docs.Document(symbol.Location.URI); !open {
			out = append(out, symbol)
			found++
		}
	}
	out = sortSymbols(out)
	return out[:min(len(out), maxWorkspaceSymbols)], nil
}
