package server

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

var supportedEncodings = []docstore.Encoding{docstore.EncodingUTF16, docstore.EncodingUTF8, docstore.EncodingUTF32}

func (s *Server) handleDidOpen(_ context.Context, params json.RawMessage) error {
	openParams, err := lsp.DecodeParams[lsp.DidOpenTextDocumentParams](params)
	if err != nil {
		return err
	}
	item := openParams.TextDocument
	s.docs.Open(item)
	s.updateDialect(item.URI, item.LanguageID, item.Text)
	s.logger.Debug("document opened", "uri", item.URI, "version", item.Version)
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
	doc, ok := s.docs.Document(changeParams.TextDocument.URI)
	if ok && doc.Text() != "" {
		s.updateDialect(doc.URI, doc.Language, doc.Text())
	}
	return nil
}

func (s *Server) updateDialect(uri, languageID, text string) {
	d := dialect.Detect(s.dialectOverride(), dialect.Input{URI: uri, LanguageID: languageID, Text: text})
	s.dialects.Store(uri, d)
	s.logger.Debug("dialect detected", "uri", uri, "dialect", d.String())
}

func (s *Server) Dialect(uri string) (dialect.Dialect, bool) {
	d, ok := s.dialects.Load(uri)
	if !ok {
		return 0, false
	}
	stored, ok := d.(dialect.Dialect)
	if !ok {
		return 0, false
	}
	return stored, true
}

func (s *Server) dialectOverride() *dialect.Dialect {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dialectForced {
		return nil
	}
	return &s.forcedDialect
}

func (s *Server) SetDialectOverride(d dialect.Dialect, set bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dialectForced = set
	s.forcedDialect = d
}

func (s *Server) handleDidClose(_ context.Context, params json.RawMessage) error {
	closeParams, err := lsp.DecodeParams[lsp.DidCloseTextDocumentParams](params)
	if err != nil {
		return err
	}
	s.docs.Close(closeParams.TextDocument.URI)
	s.dialects.Delete(closeParams.TextDocument.URI)
	return nil
}

func (s *Server) handleDidSave(_ context.Context, params json.RawMessage) error {
	saveParams, err := lsp.DecodeParams[lsp.DidSaveTextDocumentParams](params)
	if err != nil {
		return err
	}
	if _, ok := s.docs.Document(saveParams.TextDocument.URI); !ok {
		s.logger.Warn("didSave for unopened document", "uri", saveParams.TextDocument.URI)
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
