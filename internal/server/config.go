package server

import (
	"context"
	"encoding/json"

	"github.com/stubbedev/shell-lsp/internal/config"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

func (s *Server) currentSettings() (config.Settings, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings, s.generation
}

func (s *Server) handleDidChangeConfiguration(ctx context.Context, params json.RawMessage) error {
	changeParams, err := lsp.DecodeParams[lsp.DidChangeConfigurationParams](params)
	if err != nil {
		return err
	}
	settings, found, err := config.ParseSection(changeParams.Settings)
	if err != nil {
		s.notify("window/showMessage", lsp.ShowMessageParams{
			Type:    lsp.MessageError,
			Message: "Ignoring invalid configuration, keeping previous settings: " + err.Error(),
		})
		return err
	}
	if !found {
		return nil
	}
	s.mu.Lock()
	s.settings = settings
	s.generation++
	refresh := s.client.Workspace.Diagnostics.RefreshSupport
	s.mu.Unlock()
	s.workspace.InvalidateAll()
	s.refreshDiagnostics(ctx, refresh)
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
	go func() {
		if err := s.call(ctx, "workspace/diagnostic/refresh", nil, nil); err != nil {
			s.logger.Warn("requesting diagnostic refresh", "err", err)
		}
	}()
}
