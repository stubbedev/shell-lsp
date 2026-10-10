package server

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

var supportedEncodings = []docstore.Encoding{docstore.EncodingUTF16, docstore.EncodingUTF8, docstore.EncodingUTF32}

func (s *Server) handleDidOpen(_ context.Context, params json.RawMessage) error {
	openParams, err := lsp.DecodeParams[lsp.DidOpenTextDocumentParams](params)
	if err != nil {
		return err
	}
	s.docs.Open(openParams.TextDocument)
	s.logger.Debug("document opened", "uri", openParams.TextDocument.URI,
		"version", openParams.TextDocument.Version)
	return nil
}

func (s *Server) handleDidChange(_ context.Context, params json.RawMessage) error {
	changeParams, err := lsp.DecodeParams[lsp.DidChangeTextDocumentParams](params)
	if err != nil {
		return err
	}
	if err := s.docs.Change(
		changeParams.TextDocument.URI,
		changeParams.TextDocument.Version,
		changeParams.ContentChanges,
	); err != nil {
		s.logger.Warn("didChange rejected", "err", err)
	}
	return nil
}

func (s *Server) handleDidClose(_ context.Context, params json.RawMessage) error {
	closeParams, err := lsp.DecodeParams[lsp.DidCloseTextDocumentParams](params)
	if err != nil {
		return err
	}
	s.docs.Close(closeParams.TextDocument.URI)
	return nil
}

func (s *Server) handleDidSave(_ context.Context, params json.RawMessage) error {
	saveParams, err := lsp.DecodeParams[lsp.DidSaveTextDocumentParams](params)
	if err != nil {
		return err
	}
	if saveParams.Text != nil {
		uri := saveParams.TextDocument.URI
		doc, ok := s.docs.Document(uri)
		if !ok {
			s.logger.Warn("didSave for unopened document", "uri", uri)
			return nil
		}
		if err := s.docs.Change(uri, doc.Version+1, []lsp.TextDocumentContentChangeEvent{{Text: *saveParams.Text}}); err != nil {
			s.logger.Warn("didSave text rejected", "err", err)
		}
	}
	return nil
}

// negotiateEncoding picks the client's preferred encoding among the
// ones we support, defaulting to UTF-16 per the LSP specification.
func negotiateEncoding(rawCaps json.RawMessage, logger *slog.Logger) docstore.Encoding {
	if len(rawCaps) == 0 {
		return docstore.EncodingUTF16
	}
	var caps struct {
		General struct {
			PositionEncodings []string `json:"positionEncodings"`
		} `json:"general"`
	}
	if err := json.Unmarshal(rawCaps, &caps); err != nil {
		logger.Warn("parsing client capabilities", "err", err)
		return docstore.EncodingUTF16
	}
	for _, name := range caps.General.PositionEncodings {
		for _, supported := range supportedEncodings {
			if enc, err := docstore.EncodingFromName(name); err == nil && enc == supported {
				return enc
			}
		}
	}
	return docstore.EncodingUTF16
}
