package server

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/config"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/platform"
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

func (s *Server) settingsFor(at docstore.Location) (config.Settings, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !at.Local {
		return s.settings, s.generation
	}
	return settingsForPath(s.settings, s.folderSettings, at.Path), s.generation
}

func settingsForPath(global config.Settings, folders map[string]config.Settings, path string) config.Settings {
	best, settings := "", global
	for root, folder := range folders {
		if platform.Native.Within(path, root) && len(root) > len(best) {
			best, settings = root, folder
		}
	}
	return settings
}

func (s *Server) handleDidChangeConfiguration(ctx context.Context, changeParams lsp.DidChangeConfigurationParams) error {
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
	s.notify(showMessageMethod, lsp.ShowMessageParams{
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
	s.refreshDiagnostics(ctx)
	for _, refresh := range []struct {
		method    string
		supported bool
	}{
		{semanticTokensRefreshMethod, caps.SemanticTokens.RefreshSupport},
		{inlayHintRefreshMethod, caps.InlayHint.RefreshSupport},
		{codeLensRefreshMethod, caps.CodeLens.RefreshSupport},
	} {
		if refresh.supported {
			s.requestRefresh(ctx, refresh.method)
		}
	}
}

func (s *Server) requestRefresh(ctx context.Context, method string) {
	s.outbound.run(method, func() {
		if err := s.call(ctx, method, nil, nil); err != nil {
			s.logger.Warn("requesting refresh", "method", method, "err", err)
		}
	})
}

func (s *Server) pullConfiguration(ctx context.Context) {
	s.outbound.run(configurationMethod, func() { s.loadConfiguration(ctx) })
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
	if err := s.call(ctx, configurationMethod, configurationParams{Items: items}, &results); err != nil {
		s.logger.Warn("requesting "+configurationMethod, "err", err)
		return
	}
	if len(results) != len(items) {
		s.logger.Warn(configurationMethod+" answered the wrong number of items", "want", len(items), "got", len(results))
		return
	}
	folders := map[string]config.Settings{}
	var global *config.Settings
	for i, raw := range results {
		if config.IsAbsent(raw) {
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

func (s *Server) handleDidChangeWorkspaceFolders(ctx context.Context, change lsp.DidChangeWorkspaceFoldersParams) error {
	roots := s.workspace.Roots()
	for _, folder := range change.Event.Removed {
		if path, ok := s.folderPath(folder); ok {
			roots = slices.DeleteFunc(roots, func(r string) bool { return r == path })
		}
	}
	for _, folder := range change.Event.Added {
		if path, ok := s.folderPath(folder); ok && !slices.Contains(roots, path) {
			roots = append(roots, path)
		}
	}
	s.workspace.SetRoots(roots)
	s.workspace.InvalidateAll()
	s.pullConfiguration(ctx)
	return nil
}

func (s *Server) refreshDiagnostics(ctx context.Context) {
	docs := s.docs.View().Documents()
	uris := make([]string, len(docs))
	for i, doc := range docs {
		uris[i] = doc.URI
	}
	s.delivery.refresh(ctx, uris)
}
