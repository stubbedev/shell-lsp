package server

import (
	"context"
	"path/filepath"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/editorconfig"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/shellcheckrc"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

func newWorkspace(s *Server) *workspace.Workspace {
	return workspace.New(
		func(uri string) (*workspace.File, bool) {
			file, err := s.workspaceFile(s.docs.View(), uri)
			return file, err == nil
		},
		func(path string, in dialect.Input) dialect.Detection {
			s.mu.Lock()
			forced := settingsForPath(s.settings, s.folderSettings, path).Dialect
			s.mu.Unlock()
			resolved, _ := s.resolveProject(path, forced, in)
			return resolved.Detection
		},
	)
}

func (s *Server) workspaceFile(docs *docstore.View, uri string) (*workspace.File, error) {
	snap, err := s.snapshot(docs, uri)
	if err != nil {
		return nil, err
	}
	return snap.File, nil
}

func (s *Server) navigationEnv() navigation.Env {
	return navigation.Env{
		Loader:   s.workspace,
		Roots:    s.workspace.Roots(),
		Home:     s.process.Home,
		PathDirs: s.process.pathDirs(),
	}
}

func navigationHandler(mode navigation.Mode) func(*Server, context.Context, *docstore.View, lsp.TextDocumentPositionParams) (any, error) {
	return func(s *Server, ctx context.Context, docs *docstore.View, p lsp.TextDocumentPositionParams) (any, error) {
		here, inside, err := s.at(docs, p)
		if err != nil || !inside {
			return nil, err
		}
		locations, err := navigation.Definition(ctx, here.File, here.offset, s.navigationEnv(), s.candidateFiles(docs), mode)
		if err != nil {
			return nil, err
		}
		return toLocations(locations, docs), nil
	}
}

func toLocations(locations []navigation.Location, docs *docstore.View) []lsp.Location {
	out := make([]lsp.Location, len(locations))
	for i, loc := range locations {
		out[i] = lsp.Location{URI: loc.File.URI, Range: docs.Positions(loc.File.Doc).Range(loc.Span)}
	}
	return out
}

func (s *Server) handleDidChangeWatchedFiles(ctx context.Context, watched lsp.DidChangeWatchedFilesParams) error {
	projectChanged := false
	for _, change := range watched.Changes {
		path, err := fileuri.ToPath(change.URI)
		if err != nil {
			s.logger.Warn("ignoring watched file event", "uri", change.URI, "err", err)
			continue
		}
		if filepath.Base(path) == editorconfig.FileName {
			s.editorconfig.Invalidate(path)
			projectChanged = true
			continue
		}
		if shellcheckrc.IsName(path) {
			s.shellcheckrc.Invalidate(path)
			projectChanged = true
			continue
		}
		s.workspace.Invalidate(path)
	}
	if projectChanged {
		s.applySettings(ctx, func() {})
		return nil
	}
	s.refreshDiagnostics(ctx)
	return nil
}

func workspaceRoots(params InitializeParams) []string {
	var roots []string
	add := func(uri string) {
		path, err := fileuri.ToPath(uri)
		if err == nil {
			roots = append(roots, path)
		}
	}
	for _, folder := range params.WorkspaceFolders {
		add(folder.URI)
	}
	if len(roots) == 0 && params.RootURI != "" {
		add(params.RootURI)
	}
	if len(roots) == 0 && params.RootPath != "" {
		roots = append(roots, params.RootPath)
	}
	return roots
}

func (s *Server) candidateFiles(docs *docstore.View) navigation.WorkspaceFiles {
	return func(ctx context.Context) ([]*workspace.File, error) {
		paths, err := s.workspace.ShellFiles(ctx)
		if err != nil {
			return nil, err
		}
		for _, doc := range docs.Documents() {
			if path, err := fileuri.ToPath(doc.URI); err == nil {
				paths = append(paths, path)
			}
		}
		slices.Sort(paths)
		return s.workspace.LoadAll(ctx, slices.Compact(paths), interactive.workers(), func(int, int) {})
	}
}

func (s *Server) imports(file *workspace.File) []parser.Import {
	return navigation.Imports(file, s.navigationEnv())
}
