package server

import (
	"context"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

func (s *Server) handleDidOpen(_ context.Context, openParams lsp.DidOpenTextDocumentParams) error {
	item := openParams.TextDocument
	s.docs.Open(item)
	s.logger.Debug("document opened", "uri", item.URI, "version", item.Version)
	s.documentChanged(item.URI)
	go func() {
		if snap, err := s.snapshot(s.docs.View(), item.URI); err == nil {
			s.prefetchCommands(snap.Tree)
		}
	}()
	return nil
}

func (s *Server) handleDidChange(_ context.Context, changeParams lsp.DidChangeTextDocumentParams) error {
	uri := changeParams.TextDocument.URI
	if err := s.docs.Change(uri, changeParams.TextDocument.Version, changeParams.ContentChanges); err != nil {
		s.logger.Warn("didChange rejected", "err", err)
		return nil
	}
	s.documentChanged(uri)
	return nil
}

func (s *Server) handleDidClose(_ context.Context, closeParams lsp.DidCloseTextDocumentParams) error {
	uri := closeParams.TextDocument.URI
	s.docs.Close(uri)
	s.snapshots.forget(uri)
	s.semantic.forget(uri)
	s.delivery.closed(uri)
	return nil
}

func (s *Server) documentChanged(uri string) {
	s.warm(uri)
	s.delivery.changed(uri)
}

func negotiateEncoding(preferred []string) docstore.Encoding {
	for _, name := range preferred {
		if enc, err := docstore.EncodingFromName(name); err == nil {
			return enc
		}
	}
	return docstore.EncodingUTF16
}
