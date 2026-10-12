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
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/projection"
	"github.com/stubbedev/shell-lsp/internal/textedit"
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
	normalized, _ := splitLineEnds(strings.TrimPrefix(text, byteOrderMark))
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

const byteOrderMark = "\uFEFF"

type Location struct {
	URI   string
	Path  string
	Local bool
}

func LocationOf(uri string) Location {
	path, err := fileuri.ToPath(uri)
	if err != nil {
		return Location{URI: uri}
	}
	return Location{URI: uri, Path: path, Local: true}
}

type Document struct {
	Location
	Language  string
	Version   int32
	bom       bool
	text      string
	lineStart []int
	lineEnds  []lineEnd
	nonASCII  int
	source    *Document
	mapping   *projection.Map
}

func NewDocument(uri, language string, version int32, text string) *Document {
	d := &Document{Location: LocationOf(uri), Language: language, Version: version}
	d.setText(text)
	return d
}

func Project(source *Document, m *projection.Map) *Document {
	if m.Host() != source.Text() {
		panic("docstore: projecting " + source.URI + " from text other than its own")
	}
	d := &Document{Location: source.Location, Language: source.Language, Version: source.Version, source: source, mapping: m}
	d.setText(m.Text())
	return d
}

func (d *Document) Projected() bool { return d.mapping != nil }

func (d *Document) Source() *Document {
	if d.mapping == nil {
		return d
	}
	return d.source
}

func (d *Document) SourceEdits(edits []textedit.Edit) ([]textedit.Edit, bool) {
	if d.mapping == nil {
		return edits, true
	}
	return d.mapping.Edits(edits)
}

func (d *Document) SourceReplacement(e textedit.Edit) (textedit.Edit, bool) {
	if d.mapping == nil {
		return e, true
	}
	return d.mapping.Replacement(e)
}

func (d *Document) TextEdits(edits []textedit.Edit, enc Encoding) ([]lsp.TextEdit, bool) {
	mapped, ok := d.SourceEdits(edits)
	if !ok {
		return nil, false
	}
	source := d.Source()
	out := make([]lsp.TextEdit, len(mapped))
	for i, e := range mapped {
		out[i] = lsp.TextEdit{Range: source.RangeFor(e.Start, e.End, enc), NewText: e.NewText}
	}
	return out, true
}

func (d *Document) SourceOffset(offset int) int {
	if d.mapping == nil {
		return offset
	}
	return int(d.mapping.HostOffset(projection.ProjOffset(offset)))
}

func (d *Document) SourceLine(offset int) int {
	return int(d.OffsetToPosition(offset, EncodingUTF8).Line) + 1
}

func (d *Document) Synthetic(offset int) bool {
	return d.mapping != nil && d.mapping.Synthetic(projection.ProjOffset(offset))
}

func (d *Document) Locate(pos lsp.Position, enc Encoding) (int, bool) {
	if d.mapping == nil {
		return d.PositionToOffset(pos, enc), true
	}
	offset, inside := d.mapping.Offset(projection.HostOffset(d.source.PositionToOffset(pos, enc)))
	return int(offset), inside
}

func (d *Document) setText(raw string) {
	raw, d.bom = strings.CutPrefix(raw, byteOrderMark)
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

func (d *Document) ByteOrderMark() bool { return d.bom }

func (d *Document) CarriageReturns() []int {
	var out []int
	for line, end := range d.lineEnds {
		if end != endLF {
			out = append(out, d.lineStart[line+1]-1)
		}
	}
	return out
}

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
	if d.mapping != nil {
		panic("docstore: PositionToOffset on the projected " + d.URI + "; Locate says whether the position lies in its shell")
	}
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
	if d.mapping != nil {
		return d.source.OffsetToPosition(int(d.mapping.HostOffset(projection.ProjOffset(offset))), enc)
	}
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
	return lsp.Position{Line: uint32(line), Character: enc.Units(d.text[start:offset])} //nolint:gosec // line indexes into text, so it is bounded by len(text)
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

func (e Encoding) Units(s string) uint32 {
	var units uint32
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		units += codeUnits(r, size, e)
		i += size
	}
	return units
}

type Positions struct {
	doc *Document
	enc Encoding
}

func (d *Document) Positions(enc Encoding) Positions { return Positions{doc: d, enc: enc} }

func (p Positions) Document() *Document { return p.doc }

func (p Positions) Encoding() Encoding { return p.enc }

func (p Positions) Position(offset int) lsp.Position { return p.doc.OffsetToPosition(offset, p.enc) }

func (p Positions) Offset(pos lsp.Position) (int, bool) { return p.doc.Locate(pos, p.enc) }

func (p Positions) Range(span parser.Span) lsp.Range {
	return p.doc.RangeFor(span.Start, span.End, p.enc)
}

func (p Positions) Ranges(spans []parser.Span) []lsp.Range {
	out := make([]lsp.Range, len(spans))
	for i, span := range spans {
		out[i] = p.Range(span)
	}
	return out
}

func (p Positions) Span(r lsp.Range) (parser.Span, bool) {
	start, startInside := p.Offset(r.Start)
	end, endInside := p.Offset(r.End)
	return parser.Span{Start: start, End: max(start, end)}, startInside || endInside || start < end
}

func (p Positions) TextEdits(edits []textedit.Edit) ([]lsp.TextEdit, bool) {
	return p.doc.TextEdits(edits, p.enc)
}

func (d *Document) applyChanges(changes []lsp.TextDocumentContentChangeEvent, enc Encoding) {
	for _, change := range changes {
		if change.Range == nil {
			d.setText(change.Text)
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

func (v *View) Positions(d *Document) Positions { return d.Positions(v.encoding) }

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
