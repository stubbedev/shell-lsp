package server

import (
	"context"
	"encoding/json"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

func (s *Server) handleDidOpen(_ context.Context, params json.RawMessage) error {
	openParams, err := lsp.DecodeParams[lsp.DidOpenTextDocumentParams](params)
	if err != nil {
		return err
	}
	item := openParams.TextDocument
	s.docs.Open(item)
	s.logger.Debug("document opened", "uri", item.URI, "version", item.Version)
	s.documentChanged(item.URI)
	go func() {
		if snap, err := s.snapshot(s.docs.View(), item.URI); err == nil {
			s.prefetchCommands(snap.tree)
		}
	}()
	return nil
}

func (s *Server) handleDidChange(_ context.Context, params json.RawMessage) error {
	changeParams, err := lsp.DecodeParams[lsp.DidChangeTextDocumentParams](params)
	if err != nil {
		return err
	}
	uri := changeParams.TextDocument.URI
	if err := s.docs.Change(uri, changeParams.TextDocument.Version, changeParams.ContentChanges); err != nil {
		s.logger.Warn("didChange rejected", "err", err)
		return nil
	}
	s.documentChanged(uri)
	return nil
}

func (s *Server) handleDidClose(_ context.Context, params json.RawMessage) error {
	closeParams, err := lsp.DecodeParams[lsp.DidCloseTextDocumentParams](params)
	if err != nil {
		return err
	}
	uri := closeParams.TextDocument.URI
	s.docs.Close(uri)
	s.snapshots.forget(uri)
	s.semantic.forget(uri)
	if s.push != nil {
		s.push.cancel(uri)
		s.notify(publishDiagnosticsMethod, lsp.PublishDiagnosticsParams{URI: uri, Diagnostics: []lsp.Diagnostic{}})
	}
	return nil
}

func (s *Server) documentChanged(uri string) {
	s.warm(uri)
	if s.push != nil {
		s.push.schedule(uri)
	}
}

func negotiateEncoding(preferred []string) docstore.Encoding {
	for _, name := range preferred {
		if enc, err := docstore.EncodingFromName(name); err == nil {
			return enc
		}
	}
	return docstore.EncodingUTF16
}
