package server

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/config"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

type configurationItem struct {
	ScopeURI string `json:"scopeUri,omitempty"`
	Section  string `json:"section"`
}

type configurationParams struct {
	Items []configurationItem `json:"items"`
}

func (s *Server) currentSettings() (config.Settings, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings, s.generation
}

func (s *Server) settingsFor(uri string) (config.Settings, uint64) {
	path, err := fileuri.ToPath(uri)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		return s.settings, s.generation
	}
	return settingsForPath(s.settings, s.folderSettings, path), s.generation
}

func settingsForPath(global config.Settings, folders map[string]config.Settings, path string) config.Settings {
	best, settings := "", global
	for root, folder := range folders {
		if workspace.Within(path, root) && len(root) > len(best) {
			best, settings = root, folder
		}
	}
	return settings
}

func (s *Server) handleDidChangeConfiguration(ctx context.Context, params json.RawMessage) error {
	changeParams, err := lsp.DecodeParams[lsp.DidChangeConfigurationParams](params)
	if err != nil {
		return err
	}
	settings, found, err := config.ParseSection(changeParams.Settings)
	if err != nil {
		s.rejectConfiguration(err)
		return err
	}
	if !found {
		s.pullConfiguration(ctx)
		return nil
	}
	s.applySettings(ctx, func() { s.settings = settings })
	return nil
}

func (s *Server) rejectConfiguration(err error) {
	s.notify("window/showMessage", lsp.ShowMessageParams{
		Type:    lsp.MessageError,
		Message: "Ignoring invalid configuration, keeping previous settings: " + err.Error(),
	})
}

func (s *Server) applySettings(ctx context.Context, update func()) {
	s.mu.Lock()
	update()
	s.generation++
	caps := s.client.Workspace
	s.mu.Unlock()
	s.workspace.InvalidateAll()
	s.refreshDiagnostics(ctx, caps.Diagnostics.RefreshSupport)
	for _, refresh := range []struct {
		method    string
		supported bool
	}{
		{"workspace/semanticTokens/refresh", caps.SemanticTokens.RefreshSupport},
		{"workspace/inlayHint/refresh", caps.InlayHint.RefreshSupport},
		{"workspace/codeLens/refresh", caps.CodeLens.RefreshSupport},
	} {
		if refresh.supported {
			s.requestRefresh(ctx, refresh.method)
		}
	}
}

func (s *Server) requestRefresh(ctx context.Context, method string) {
	go func() {
		if err := s.call(ctx, method, nil, nil); err != nil {
			s.logger.Warn("requesting refresh", "method", method, "err", err)
		}
	}()
}

func (s *Server) pullConfiguration(ctx context.Context) {
	go s.loadConfiguration(ctx)
}

func (s *Server) loadConfiguration(ctx context.Context) {
	if !s.clientCapabilities().Workspace.Configuration {
		return
	}
	roots := s.workspace.Roots()
	items := []configurationItem{{Section: config.Section}}
	for _, root := range roots {
		items = append(items, configurationItem{ScopeURI: fileuri.FromPath(root), Section: config.Section})
	}
	var results []json.RawMessage
	if err := s.call(ctx, "workspace/configuration", configurationParams{Items: items}, &results); err != nil {
		s.logger.Warn("requesting workspace/configuration", "err", err)
		return
	}
	if len(results) != len(items) {
		s.logger.Warn("workspace/configuration answered the wrong number of items", "want", len(items), "got", len(results))
		return
	}
	folders := map[string]config.Settings{}
	var global *config.Settings
	for i, raw := range results {
		if isNull(raw) {
			continue
		}
		parsed, err := config.Parse(raw)
		if err != nil {
			s.rejectConfiguration(err)
			return
		}
		if i == 0 {
			global = &parsed
			continue
		}
		folders[roots[i-1]] = parsed
	}
	s.applySettings(ctx, func() {
		if global != nil {
			s.settings = *global
		}
		s.folderSettings = folders
	})
}

func isNull(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed == "" || trimmed == "null"
}

func (s *Server) handleDidChangeWorkspaceFolders(ctx context.Context, params json.RawMessage) error {
	change, err := lsp.DecodeParams[lsp.DidChangeWorkspaceFoldersParams](params)
	if err != nil {
		return err
	}
	roots := s.workspace.Roots()
	for _, folder := range change.Event.Removed {
		if path, err := fileuri.ToPath(folder.URI); err == nil {
			roots = slices.DeleteFunc(roots, func(r string) bool { return r == path })
		}
	}
	for _, folder := range change.Event.Added {
		if path, err := fileuri.ToPath(folder.URI); err == nil && !slices.Contains(roots, path) {
			roots = append(roots, path)
		}
	}
	s.workspace.SetRoots(roots)
	s.workspace.InvalidateAll()
	s.pullConfiguration(ctx)
	return nil
}

func (s *Server) refreshDiagnostics(ctx context.Context, clientRefresh bool) {
	if s.push != nil {
		for _, doc := range s.docs.View().Documents() {
			s.push.schedule(doc.URI)
		}
		return
	}
	if !clientRefresh {
		return
	}
	s.requestRefresh(ctx, "workspace/diagnostic/refresh")
}
