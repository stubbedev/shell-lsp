package server

import (
	"fmt"
	"sync"

	"github.com/stubbedev/shell-lsp/internal/config"
	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

type snapshot struct {
	doc        *docstore.Document
	encoding   docstore.Encoding
	dialect    dialect.Dialect
	tree       *parser.Tree
	settings   config.Settings
	generation uint64
}

type snapshotCache struct {
	mu      sync.Mutex
	entries map[string]*snapshot
}

func newSnapshotCache() *snapshotCache {
	return &snapshotCache{entries: map[string]*snapshot{}}
}

func (c *snapshotCache) lookup(uri string, doc *docstore.Document, generation uint64) (*snapshot, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	snap, ok := c.entries[uri]
	if !ok || snap.doc != doc || snap.generation != generation {
		return nil, false
	}
	return snap, true
}

func (c *snapshotCache) store(uri string, snap *snapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if current, ok := c.entries[uri]; ok && current.doc.Version > snap.doc.Version {
		return
	}
	c.entries[uri] = snap
}

func (c *snapshotCache) forget(uri string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, uri)
}

func (s *Server) snapshot(docs *docstore.View, uri string) (*snapshot, error) {
	doc, ok := docs.Document(uri)
	if !ok {
		return nil, fmt.Errorf("document %s is not open", uri)
	}
	settings, generation := s.currentSettings()
	if snap, ok := s.snapshots.lookup(uri, doc, generation); ok {
		return snap, nil
	}
	d := dialect.Detect(settings.Dialect, dialect.Input{URI: uri, LanguageID: doc.Language, Text: doc.Text()})
	snap := &snapshot{
		doc:        doc,
		encoding:   docs.Encoding(),
		dialect:    d,
		tree:       parser.Parse(doc.Text(), d),
		settings:   settings,
		generation: generation,
	}
	s.snapshots.store(uri, snap)
	return snap, nil
}
