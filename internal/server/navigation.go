package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/editorconfig"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/project"
	"github.com/stubbedev/shell-lsp/internal/shellcheckrc"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

func newWorkspace(s *Server) *workspace.Workspace {
	return workspace.New(
		func(uri string) (*workspace.File, bool) {
			file, err := s.workspaceFile(s.docs.View(), uri)
			return file, err == nil
		},
		func(path string, in dialect.Input) dialect.Dialect {
			s.mu.Lock()
			forced := settingsForPath(s.settings, s.folderSettings, path).Dialect
			s.mu.Unlock()
			resolved, err := project.Resolve(s.editorconfig, path, forced, in)
			if err != nil {
				s.noticeOnce("editorconfig:"+err.Error(), lsp.MessageWarning, err.Error())
			}
			return resolved.Dialect
		},
	)
}

func (s *Server) workspaceFile(docs *docstore.View, uri string) (*workspace.File, error) {
	snap, err := s.snapshot(docs, uri)
	if err != nil {
		return nil, err
	}
	return snap.file, nil
}

func (s *Server) navigationEnv() navigation.Env {
	return navigation.Env{
		Loader:   s.workspace,
		Roots:    s.workspace.Roots(),
		Home:     s.process.Home,
		PathDirs: s.process.pathDirs(),
	}
}

func (s *Server) navigationHandler(mode navigation.Mode) requestHandler {
	return func(ctx context.Context, docs *docstore.View, params json.RawMessage) (any, error) {
		position, err := lsp.DecodeParams[lsp.TextDocumentPositionParams](params)
		if err != nil {
			return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing position params: %v", err)
		}
		file, err := s.workspaceFile(docs, position.TextDocument.URI)
		if err != nil {
			return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%v", err)
		}
		offset, inside := file.Doc.Locate(position.Position, docs.Encoding())
		if !inside {
			return nil, nil
		}
		locations, err := navigation.Definition(ctx, file, offset, s.navigationEnv(), s.candidateFiles(docs), mode)
		if err != nil {
			return nil, err
		}
		return toLocations(locations, docs.Encoding()), nil
	}
}

func toLocations(locations []navigation.Location, enc docstore.Encoding) []lsp.Location {
	out := make([]lsp.Location, len(locations))
	for i, loc := range locations {
		out[i] = lsp.Location{URI: loc.File.URI, Range: loc.File.Doc.RangeFor(loc.Span.Start, loc.Span.End, enc)}
	}
	return out
}

func (s *Server) handleDidChangeWatchedFiles(ctx context.Context, params json.RawMessage) error {
	watched, err := lsp.DecodeParams[lsp.DidChangeWatchedFilesParams](params)
	if err != nil {
		return err
	}
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
	s.refreshDiagnostics(ctx, s.clientCapabilities().Workspace.Diagnostics.RefreshSupport)
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
