package docstore

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/fileuri"
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

func NormalizeCanonical(text string) string {
	normalized, _ := splitLineEnds(strings.TrimPrefix(text, "\uFEFF"))
	return normalized
}

type lineEnd uint8

const (
	endLF lineEnd = iota
	endCR
	endCRLF
)

func splitLineEnds(raw string) (string, []lineEnd) {
	if !strings.ContainsRune(raw, '\r') {
		return raw, nil
	}
	var sb strings.Builder
	sb.Grow(len(raw))
	ends := make([]lineEnd, 0, strings.Count(raw, "\n")+strings.Count(raw, "\r"))
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '\r':
			if i+1 < len(raw) && raw[i+1] == '\n' {
				ends = append(ends, endCRLF)
				i++
			} else {
				ends = append(ends, endCR)
			}
			sb.WriteByte('\n')
		case '\n':
			ends = append(ends, endLF)
			sb.WriteByte('\n')
		default:
			sb.WriteByte(raw[i])
		}
	}
	return sb.String(), ends
}

type Document struct {
	URI       string
	Language  string
	Version   int32
	text      string
	lineStart []int
	lineEnds  []lineEnd
	nonASCII  int
}

func NewDocument(uri, language string, version int32, text string) *Document {
	d := &Document{URI: uri, Language: language, Version: version}
	d.setText(strings.TrimPrefix(text, "\uFEFF"))
	return d
}

func (d *Document) setText(raw string) {
	text, ends := splitLineEnds(raw)
	d.text = text
	d.lineEnds = ends
	d.lineStart = appendLineStarts(make([]int, 1, strings.Count(text, "\n")+1), text, 0)
	d.nonASCII = countNonASCII(text)
}

func countNonASCII(s string) int {
	n := 0
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			n++
		}
	}
	return n
}

func appendLineStarts(starts []int, text string, base int) []int {
	for i := strings.IndexByte(text, '\n'); i >= 0; {
		starts = append(starts, base+i+1)
		next := strings.IndexByte(text[i+1:], '\n')
		if next < 0 {
			break
		}
		i += next + 1
	}
	return starts
}

func (d *Document) replaceRaw(start, end int, raw string) {
	if d.lineEnds == nil && !strings.ContainsRune(raw, '\r') {
		d.replace(start, end, raw)
		return
	}
	first := sort.SearchInts(d.lineStart, start+1) - 1
	last := sort.SearchInts(d.lineStart, end+1) - 1
	var prefix, suffix string
	if start == d.lineStart[first] && first > 0 && d.lineEndKind(first-1) == endCR {
		start--
		first--
		prefix = "\r"
	}
	if end == d.lineEnd(last) && last+1 < len(d.lineStart) && d.lineEndKind(last) == endLF {
		end++
		suffix = "\n"
	}
	text, ends := splitLineEnds(prefix + raw + suffix)
	if ends == nil {
		ends = make([]lineEnd, strings.Count(text, "\n"))
	}
	current := d.lineEnds
	if current == nil {
		current = make([]lineEnd, len(d.lineStart)-1)
	}
	removed := strings.Count(d.text[start:end], "\n")
	d.lineEnds = slices.Concat(current[:first], ends, current[first+removed:])
	d.replace(start, end, text)
}

func (d *Document) lineEndKind(line int) lineEnd {
	if d.lineEnds == nil {
		return endLF
	}
	return d.lineEnds[line]
}

func (d *Document) replace(start, end int, text string) {
	kept := sort.SearchInts(d.lineStart, start+1)
	tail := d.lineStart[sort.SearchInts(d.lineStart, end+1):]
	starts := make([]int, kept, kept+strings.Count(text, "\n")+len(tail))
	copy(starts, d.lineStart[:kept])
	starts = appendLineStarts(starts, text, start)
	shift := len(text) - (end - start)
	for _, s := range tail {
		starts = append(starts, s+shift)
	}
	d.nonASCII += countNonASCII(text) - countNonASCII(d.text[start:end])
	d.text = d.text[:start] + text + d.text[end:]
	d.lineStart = starts
}

func (d *Document) Text() string { return d.text }

func (d *Document) LineCount() int { return len(d.lineStart) }

func (d *Document) LineBounds(line int) (int, int) {
	if line < 0 || line >= len(d.lineStart) {
		return len(d.text), len(d.text)
	}
	return d.lineStart[line], d.lineEnd(line)
}

func (d *Document) lineEnd(line int) int {
	if line+1 < len(d.lineStart) {
		return d.lineStart[line+1] - 1
	}
	return len(d.text)
}

func (d *Document) PositionToOffset(pos lsp.Position, enc Encoding) int {
	line := int(pos.Line)
	if line >= len(d.lineStart) {
		return len(d.text)
	}
	start, end := d.lineStart[line], d.lineEnd(line)
	if d.nonASCII == 0 {
		return min(start+int(pos.Character), end)
	}
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

func (d *Document) OffsetToPosition(offset int, enc Encoding) lsp.Position {
	if offset > len(d.text) {
		offset = len(d.text)
	}
	if offset < 0 {
		offset = 0
	}
	line, _ := slices.BinarySearch(d.lineStart, offset+1)
	line--
	start := d.lineStart[line]
	if d.nonASCII == 0 {
		return lsp.Position{Line: uint32(line), Character: uint32(offset - start)} //nolint:gosec // line and column index into text, so they are bounded by len(text)
	}
	var units uint32
	for i := start; i < offset; {
		r, size := utf8.DecodeRuneInString(d.text[i:])
		units += codeUnits(r, size, enc)
		i += size
	}
	return lsp.Position{Line: uint32(line), Character: units} //nolint:gosec // line indexes into text, so it is bounded by len(text)
}

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
			d.setText(strings.TrimPrefix(change.Text, "\uFEFF"))
			continue
		}
		start := d.PositionToOffset(change.Range.Start, enc)
		end := d.PositionToOffset(change.Range.End, enc)
		if end < start {
			start, end = end, start
		}
		d.replaceRaw(start, end, change.Text)
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

type View struct {
	encoding Encoding
	docs     map[string]*Document
}

func (v *View) Encoding() Encoding { return v.encoding }

func (v *View) Document(uri string) (*Document, bool) {
	d, ok := v.docs[fileuri.Key(uri)]
	return d, ok
}

func (v *View) Documents() []*Document {
	docs := make([]*Document, 0, len(v.docs))
	for _, d := range v.docs {
		docs = append(docs, d)
	}
	slices.SortFunc(docs, func(a, b *Document) int { return strings.Compare(a.URI, b.URI) })
	return docs
}

type Store struct {
	mu   sync.Mutex
	view atomic.Pointer[View]
}

func NewStore() *Store {
	s := &Store{}
	s.view.Store(&View{encoding: EncodingUTF16, docs: map[string]*Document{}})
	return s
}

func (s *Store) View() *View { return s.view.Load() }

func (s *Store) Has(uri string) bool {
	_, ok := s.View().Document(uri)
	return ok
}

func (s *Store) update(fn func(next *View) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.view.Load()
	next := &View{encoding: current.encoding, docs: maps.Clone(current.docs)}
	if err := fn(next); err != nil {
		return err
	}
	s.view.Store(next)
	return nil
}

func (s *Store) SetEncoding(enc Encoding) {
	_ = s.update(func(next *View) error {
		next.encoding = enc
		return nil
	})
}

func (s *Store) Open(item lsp.TextDocumentItem) {
	_ = s.update(func(next *View) error {
		next.docs[fileuri.Key(item.URI)] = NewDocument(item.URI, item.LanguageID, item.Version, item.Text)
		return nil
	})
}

func (s *Store) Close(uri string) {
	_ = s.update(func(next *View) error {
		delete(next.docs, fileuri.Key(uri))
		return nil
	})
}

func (s *Store) Change(uri string, version int32, changes []lsp.TextDocumentContentChangeEvent) error {
	return s.update(func(next *View) error {
		k := fileuri.Key(uri)
		d, ok := next.docs[k]
		if !ok {
			return fmt.Errorf("didChange for unopened document %s", uri)
		}
		if version <= d.Version {
			return fmt.Errorf("ignoring stale change for %s: got version %d, have %d", uri, version, d.Version)
		}
		updated := *d
		updated.Version = version
		updated.applyChanges(changes, next.encoding)
		next.docs[k] = &updated
		return nil
	})
}
