// Package docstore tracks open text documents: versioned incremental
// updates, line-ending and BOM normalization, and position mapping
// between LSP positions (UTF-8, UTF-16 or UTF-32 code units) and byte
// offsets.
package docstore

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/lsp"
)

type Encoding uint8

const (
	EncodingUTF8 Encoding = iota
	EncodingUTF16
	EncodingUTF32
)

func (e Encoding) String() string {
	switch e {
	case EncodingUTF8:
		return "utf-8"
	case EncodingUTF16:
		return "utf-16"
	case EncodingUTF32:
		return "utf-32"
	}
	return "unknown"
}

func EncodingFromName(name string) (Encoding, error) {
	switch name {
	case "utf-8":
		return EncodingUTF8, nil
	case "utf-16":
		return EncodingUTF16, nil
	case "utf-32":
		return EncodingUTF32, nil
	}
	return 0, fmt.Errorf("unsupported position encoding %q", name)
}

// NormalizeCanonical strips a leading UTF-8 BOM and rewrites CRLF and
// lone CR line endings to LF. Positions, parses and edits all operate
// on the normalized form.
func NormalizeCanonical(text string) string {
	text = strings.TrimPrefix(text, "\uFEFF")
	if !strings.ContainsRune(text, '\r') {
		return text
	}
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
}

type Document struct {
	URI       string
	Language  string
	Version   int32
	text      string
	lineStart []int
}

func newDocument(uri, language string, version int32, text string) *Document {
	d := &Document{URI: uri, Language: language, Version: version}
	d.setText(NormalizeCanonical(text))
	return d
}

func (d *Document) setText(text string) {
	d.text = text
	d.lineStart = d.lineStart[:0]
	for i := range len(text) {
		if text[i] == '\n' {
			d.lineStart = append(d.lineStart, i+1)
		}
	}
	d.lineStart = append([]int{0}, d.lineStart...)
}

func (d *Document) Text() string { return d.text }

func (d *Document) LineCount() int { return len(d.lineStart) }

func (d *Document) lineEnd(line int) int {
	if line+1 < len(d.lineStart) {
		return d.lineStart[line+1] - 1
	}
	return len(d.text)
}

// PositionToOffset maps an LSP position to a byte offset, clamping
// out-of-range lines and characters per the spec. The encoding selects
// the unit the character field is counted in.
func (d *Document) PositionToOffset(pos lsp.Position, enc Encoding) int {
	line := int(pos.Line)
	if line >= len(d.lineStart) {
		return len(d.text)
	}
	start, end := d.lineStart[line], d.lineEnd(line)
	offset := start
	var units uint32
	for offset < end {
		r, size := utf8.DecodeRuneInString(d.text[offset:end])
		next := units + codeUnits(r, size, enc)
		if next > pos.Character {
			break
		}
		units = next
		offset += size
	}
	return offset
}

// OffsetToPosition maps a byte offset to an LSP position in the given
// encoding. Offsets beyond the text clamp to its end.
func (d *Document) OffsetToPosition(offset int, enc Encoding) lsp.Position {
	if offset > len(d.text) {
		offset = len(d.text)
	}
	if offset < 0 {
		offset = 0
	}
	line := sort.Search(len(d.lineStart), func(i int) bool { return d.lineStart[i] > offset }) - 1
	start := d.lineStart[line]
	var units uint32
	for i := start; i < offset; {
		r, size := utf8.DecodeRuneInString(d.text[i:])
		units += codeUnits(r, size, enc)
		i += size
	}
	return lsp.Position{Line: uint32(line), Character: units} //nolint:gosec // line indexes into text, so it is bounded by len(text)
}

// RangeFor returns the LSP range covering the byte-offset interval,
// excluding any trailing newline.
func (d *Document) RangeFor(start, end int, enc Encoding) lsp.Range {
	if end < start {
		start, end = end, start
	}
	return lsp.Range{
		Start: d.OffsetToPosition(start, enc),
		End:   d.OffsetToPosition(end, enc),
	}
}

func (d *Document) applyChanges(changes []lsp.TextDocumentContentChangeEvent, enc Encoding) {
	for _, change := range changes {
		if change.Range == nil {
			d.setText(NormalizeCanonical(change.Text))
			continue
		}
		start := d.PositionToOffset(change.Range.Start, enc)
		end := d.PositionToOffset(change.Range.End, enc)
		if end < start {
			start, end = end, start
		}
		d.setText(d.text[:start] + NormalizeCanonical(change.Text) + d.text[end:])
	}
}

func codeUnits(r rune, size int, enc Encoding) uint32 {
	switch enc {
	case EncodingUTF8:
		return uint32(size) //nolint:gosec // size is the width of one rune, at most 4
	case EncodingUTF32:
		return 1
	case EncodingUTF16:
		if r > 0xFFFF {
			return 2
		}
		return 1
	}
	return 1
}

type Store struct {
	mu       sync.RWMutex
	encoding Encoding
	docs     map[string]*Document
}

func NewStore() *Store {
	return &Store{encoding: EncodingUTF16, docs: make(map[string]*Document)}
}

func (s *Store) SetEncoding(enc Encoding) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.encoding = enc
}

func (s *Store) Encoding() Encoding {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.encoding
}

func (s *Store) Open(item lsp.TextDocumentItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.docs[item.URI] = newDocument(item.URI, item.LanguageID, item.Version, item.Text)
}

func (s *Store) Close(uri string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.docs, uri)
}

// Document returns the current snapshot of an open document. Snapshots
// are immutable: later changes produce new snapshots.
func (s *Store) Document(uri string) (*Document, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.docs[uri]
	return d, ok
}

func (s *Store) Documents() []*Document {
	s.mu.RLock()
	defer s.mu.RUnlock()
	docs := make([]*Document, 0, len(s.docs))
	for _, d := range s.docs {
		docs = append(docs, d)
	}
	return docs
}

// Change applies incremental or full content changes. Versions must
// increase; stale or repeated versions are rejected so replays and
// races cannot rewind document state.
func (s *Store) Change(uri string, version int32, changes []lsp.TextDocumentContentChangeEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.docs[uri]
	if !ok {
		return fmt.Errorf("didChange for unopened document %s", uri)
	}
	if version <= d.Version {
		return fmt.Errorf("ignoring stale change for %s: got version %d, have %d", uri, version, d.Version)
	}
	updated := &Document{URI: d.URI, Language: d.Language, Version: version}
	updated.setText(d.text)
	updated.applyChanges(changes, s.encoding)
	s.docs[uri] = updated
	return nil
}
