package server

import (
	"fmt"
	"sync"

	"github.com/stubbedev/shell-lsp/internal/config"
	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/format"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/project"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

type snapshot struct {
	file       *workspace.File
	doc        *docstore.Document
	encoding   docstore.Encoding
	detection  dialect.Detection
	tree       *parser.Tree
	settings   config.Settings
	generation uint64
	project    project.Settings
	projectErr error
}

func (s *snapshot) formatStyle() format.Style {
	return s.settings.Format.Style.Over(s.project.Style)
}

type snapshotEntry struct {
	doc        *docstore.Document
	generation uint64
	ready      chan struct{}
	snap       *snapshot
}

type snapshotCache struct {
	open    func(uri string) bool
	mu      sync.Mutex
	entries map[string]*snapshotEntry
}

func newSnapshotCache(open func(uri string) bool) *snapshotCache {
	return &snapshotCache{open: open, entries: map[string]*snapshotEntry{}}
}

func (c *snapshotCache) get(uri string, doc *docstore.Document, generation uint64, build func(previous *snapshot) *snapshot) *snapshot {
	for {
		e, owner, previous := c.claim(uri, doc, generation)
		if owner {
			return c.fill(uri, e, func() *snapshot { return build(previous) })
		}
		<-e.ready
		if e.snap != nil {
			return e.snap
		}
	}
}

func (c *snapshotCache) claim(uri string, doc *docstore.Document, generation uint64) (*snapshotEntry, bool, *snapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	current, ok := c.entries[uri]
	if ok && current.doc == doc && current.generation == generation {
		return current, false, nil
	}
	e := &snapshotEntry{doc: doc, generation: generation, ready: make(chan struct{})}
	if !c.open(uri) {
		return e, true, nil
	}
	if !ok || current.doc.Version <= doc.Version {
		c.entries[uri] = e
	}
	if !ok {
		return e, true, nil
	}
	select {
	case <-current.ready:
		return e, true, current.snap
	default:
		return e, true, nil
	}
}

func (c *snapshotCache) fill(uri string, e *snapshotEntry, build func() *snapshot) *snapshot {
	defer close(e.ready)
	defer func() {
		if e.snap != nil {
			return
		}
		c.mu.Lock()
		if c.entries[uri] == e {
			delete(c.entries, uri)
		}
		c.mu.Unlock()
	}()
	e.snap = build()
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
	uri = doc.URI
	settings, generation := s.settingsFor(uri)
	return s.snapshots.get(uri, doc, generation, func(previous *snapshot) *snapshot {
		path, err := fileuri.ToPath(uri)
		if err != nil {
			path = ""
		}
		in := dialect.Input{URI: uri, LanguageID: doc.Language, Text: doc.Text()}
		resolved, projectErr := project.Resolve(s.editorconfig, path, settings.Dialect, in)
		if projectErr != nil {
			s.noticeOnce("editorconfig:"+projectErr.Error(), lsp.MessageWarning, projectErr.Error())
		}
		var like *parser.Tree
		if previous != nil {
			like = previous.tree
		}
		d := resolved.Dialect
		tree := parser.ParseEdit(like, doc.Text(), d, parser.Variant(d, in))
		return &snapshot{
			file:       &workspace.File{Path: path, URI: uri, Doc: doc, Tree: tree},
			doc:        doc,
			encoding:   docs.Encoding(),
			detection:  resolved.Detection,
			tree:       tree,
			settings:   settings,
			generation: generation,
			project:    resolved.Settings,
			projectErr: projectErr,
		}
	}), nil
}

func (s *Server) warm(uri string) {
	s.warming.run(uri, func() {
		if _, err := s.snapshot(s.docs.View(), uri); err != nil {
			s.logger.Debug("warming closed document", "uri", uri)
		}
	})
}
