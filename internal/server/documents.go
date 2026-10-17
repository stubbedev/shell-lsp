package server

import (
	"context"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/host"
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
	view := s.docs.View()
	doc, ok := view.Document(uri)
	if !ok {
		return
	}
	language, ok := host.Of(doc.URI, doc.Language)
	if !ok {
		return
	}
	var related []string
	for _, other := range view.Documents() {
		if l, ok := host.Of(other.URI, other.Language); ok && l == language && other.URI != uri {
			related = append(related, other.URI)
		}
	}
	if len(related) > 0 {
		s.delivery.refresh(context.Background(), related)
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
