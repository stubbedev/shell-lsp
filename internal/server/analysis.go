package server

import (
	"fmt"
	"sync"

	"github.com/stubbedev/shell-lsp/internal/config"
	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

type snapshot struct {
	file       *workspace.File
	doc        *docstore.Document
	encoding   docstore.Encoding
	dialect    dialect.Dialect
	tree       *parser.Tree
	settings   config.Settings
	generation uint64
}

type snapshotEntry struct {
	doc        *docstore.Document
	generation uint64
	ready      chan struct{}
	snap       *snapshot
}

type snapshotCache struct {
	mu      sync.Mutex
	entries map[string]*snapshotEntry
}

func newSnapshotCache() *snapshotCache {
	return &snapshotCache{entries: map[string]*snapshotEntry{}}
}

func (c *snapshotCache) get(uri string, doc *docstore.Document, generation uint64, build func() *snapshot) *snapshot {
	c.mu.Lock()
	if e, ok := c.entries[uri]; ok && e.doc == doc && e.generation == generation {
		c.mu.Unlock()
		<-e.ready
		return e.snap
	}
	e := &snapshotEntry{doc: doc, generation: generation, ready: make(chan struct{})}
	if current, ok := c.entries[uri]; !ok || current.doc.Version <= doc.Version {
		c.entries[uri] = e
	}
	c.mu.Unlock()
	e.snap = build()
	close(e.ready)
	return e.snap
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
	settings, generation := s.settingsFor(uri)
	return s.snapshots.get(uri, doc, generation, func() *snapshot {
		d := dialect.Detect(settings.Dialect, dialect.Input{URI: uri, LanguageID: doc.Language, Text: doc.Text()})
		tree := parser.Parse(doc.Text(), d)
		path, err := fileuri.ToPath(uri)
		if err != nil {
			path = ""
		}
		return &snapshot{
			file:       &workspace.File{Path: path, URI: uri, Doc: doc, Tree: tree},
			doc:        doc,
			encoding:   docs.Encoding(),
			dialect:    d,
			tree:       tree,
			settings:   settings,
			generation: generation,
		}
	}), nil
}

type warmer struct {
	mu      sync.Mutex
	running map[string]bool
	again   map[string]bool
}

func newWarmer() *warmer {
	return &warmer{running: map[string]bool{}, again: map[string]bool{}}
}

func (s *Server) warm(uri string) {
	w := s.warmer
	w.mu.Lock()
	if w.running[uri] {
		w.again[uri] = true
		w.mu.Unlock()
		return
	}
	w.running[uri] = true
	w.mu.Unlock()
	go func() {
		for {
			if _, err := s.snapshot(s.docs.View(), uri); err != nil {
				s.logger.Debug("warming closed document", "uri", uri)
			}
			w.mu.Lock()
			if !w.again[uri] {
				delete(w.running, uri)
				w.mu.Unlock()
				return
			}
			delete(w.again, uri)
			w.mu.Unlock()
		}
	}()
}
