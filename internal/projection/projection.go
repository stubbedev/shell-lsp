package projection

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

type kind uint8

const (
	verbatim kind = iota + 1
	escape
	opaque
)

type Encoder func(host string, start, end int, text string) (string, bool)

type span struct {
	kind    kind
	region  int
	proj    int
	projLen int
	host    int
	hostLen int
}

func (s span) projEnd() int { return s.proj + s.projLen }

func (s span) hostEnd() int { return s.host + s.hostLen }

func (s span) editable() bool { return s.kind == verbatim || s.kind == escape }

type Map struct {
	host     string
	text     string
	spans    []span
	encoders []Encoder
	scripts  []parser.Span
}

func (m *Map) Host() string { return m.host }

func (m *Map) Text() string { return m.text }

const noRegion = -1

type Builder struct {
	host     string
	text     strings.Builder
	spans    []span
	encoders []Encoder
	starts   []int
	at       int
	quotes   quoting
}

const Dynamic = "$$"

func NewBuilder(host string) *Builder {
	return &Builder{host: host}
}

func (b *Builder) Region(encode Encoder) {
	b.encoders = append(b.encoders, encode)
}

func (b *Builder) Script() {
	b.starts = append(b.starts, b.text.Len())
	b.quotes = quoting{}
}

func (b *Builder) Placeholder(start, end int) {
	if b.quotes.inSingle() {
		b.Opaque(start, end, "'"+Dynamic+"'")
		return
	}
	b.Opaque(start, end, Dynamic)
}

func (b *Builder) Verbatim(start, end int) {
	if start == end {
		return
	}
	b.add(verbatim, start, end, b.host[start:end])
}

func (b *Builder) Escape(start, end int, text string) {
	b.add(escape, start, end, text)
}

func (b *Builder) Opaque(start, end int, text string) {
	b.add(opaque, start, end, text)
}

func (b *Builder) add(k kind, start, end int, text string) {
	if start < b.at || end < start || end > len(b.host) {
		panic(fmt.Sprintf("projection: span %d-%d is out of order after host offset %d of %d", start, end, b.at, len(b.host)))
	}
	region := len(b.encoders) - 1
	if k != opaque && region == noRegion {
		panic(fmt.Sprintf("projection: editable span %d-%d precedes the first region", start, end))
	}
	s := span{kind: k, region: region, proj: b.text.Len(), projLen: len(text), host: start, hostLen: end - start}
	b.text.WriteString(text)
	b.quotes.feed(text)
	b.at = end
	if n := len(b.spans); n > 0 && k == verbatim {
		last := &b.spans[n-1]
		if last.kind == verbatim && last.region == region && last.hostEnd() == start {
			last.projLen += s.projLen
			last.hostLen += s.hostLen
			return
		}
	}
	b.spans = append(b.spans, s)
}

func (b *Builder) Map() *Map {
	return &Map{host: b.host, text: b.text.String(), spans: b.spans, encoders: b.encoders, scripts: b.scripts()}
}

func (b *Builder) scripts() []parser.Span {
	bounds := slices.Compact(append(append([]int{0}, b.starts...), b.text.Len()))
	out := make([]parser.Span, 0, len(bounds)-1)
	for i := 1; i < len(bounds); i++ {
		out = append(out, parser.Span{Start: bounds[i-1], End: bounds[i]})
	}
	return out
}

func (m *Map) Scripts() []parser.Span { return m.scripts }

func (m *Map) Synthetic(offset int) bool {
	i := sort.Search(len(m.spans), func(i int) bool { return m.spans[i].projEnd() > offset })
	return i < len(m.spans) && m.spans[i].hostLen == 0
}

func (m *Map) HostOffset(offset int) int {
	i := sort.Search(len(m.spans), func(i int) bool { return m.spans[i].projEnd() > offset })
	if i == len(m.spans) {
		for _, s := range slices.Backward(m.spans) {
			if s.projLen > 0 {
				return s.hostEnd()
			}
		}
		return 0
	}
	s := m.spans[i]
	if s.kind == verbatim {
		return s.host + offset - s.proj
	}
	return s.host
}

func (m *Map) Offset(host int) (int, bool) {
	i := sort.Search(len(m.spans), func(i int) bool { return m.spans[i].hostEnd() > host })
	within := i < len(m.spans) && m.spans[i].host <= host
	if within && m.spans[i].projLen > 0 {
		s := m.spans[i]
		if s.kind == verbatim {
			return s.proj + host - s.host, true
		}
		return s.proj, true
	}
	for j := i - 1; j >= 0; j-- {
		s := m.spans[j]
		if s.hostLen == 0 {
			continue
		}
		if s.hostEnd() == host && s.editable() {
			return s.projEnd(), true
		}
		break
	}
	if i < len(m.spans) {
		return m.spans[i].proj, false
	}
	return len(m.text), false
}

func (m *Map) Edit(e textedit.Edit) (textedit.Edit, bool) {
	start, end, text := m.trim(e)
	return m.Replacement(textedit.Edit{Start: start, End: end, NewText: text})
}

func (m *Map) Replacement(e textedit.Edit) (textedit.Edit, bool) {
	start, end, text := e.Start, e.End, e.NewText
	if start == end {
		return m.insert(start, text)
	}
	first := sort.Search(len(m.spans), func(i int) bool { return m.spans[i].projEnd() > start })
	var covered []span
	for _, s := range m.spans[first:] {
		if s.proj >= end {
			break
		}
		if s.projLen == 0 && s.proj == start {
			continue
		}
		covered = append(covered, s)
	}
	if len(covered) == 0 {
		return textedit.Edit{}, false
	}
	for i, s := range covered {
		if !s.editable() || s.region != covered[0].region {
			return textedit.Edit{}, false
		}
		if s.kind == escape && (start > s.proj || s.projEnd() > end) {
			return textedit.Edit{}, false
		}
		if i > 0 && covered[i-1].hostEnd() != s.host {
			return textedit.Edit{}, false
		}
	}
	firstSpan, lastSpan := covered[0], covered[len(covered)-1]
	hostStart, hostEnd := firstSpan.host, lastSpan.hostEnd()
	if firstSpan.kind == verbatim {
		hostStart += start - firstSpan.proj
	}
	if lastSpan.kind == verbatim {
		hostEnd = lastSpan.host + end - lastSpan.proj
	}
	return m.encode(firstSpan.region, hostStart, hostEnd, text)
}

func (m *Map) insert(offset int, text string) (textedit.Edit, bool) {
	var at *span
	for i := sort.Search(len(m.spans), func(i int) bool { return m.spans[i].projEnd() >= offset }); i < len(m.spans); i++ {
		s := &m.spans[i]
		if s.proj > offset {
			break
		}
		if !s.editable() || (s.kind == escape && offset != s.proj && offset != s.projEnd()) {
			continue
		}
		at = s
		if offset < s.projEnd() {
			break
		}
	}
	if at == nil {
		return textedit.Edit{}, false
	}
	host := at.hostEnd()
	switch {
	case at.kind == verbatim:
		host = at.host + offset - at.proj
	case offset == at.proj:
		host = at.host
	}
	return m.encode(at.region, host, host, text)
}

func (m *Map) encode(region, start, end int, text string) (textedit.Edit, bool) {
	encoded, ok := m.encoders[region](m.host, start, end, text)
	if !ok {
		return textedit.Edit{}, false
	}
	return textedit.Edit{Start: start, End: end, NewText: encoded}, true
}

func (m *Map) trim(e textedit.Edit) (int, int, string) {
	old, text := m.text[e.Start:e.End], e.NewText
	prefix := 0
	for prefix < len(old) && prefix < len(text) {
		r, size := utf8.DecodeRuneInString(old[prefix:])
		if s, n := utf8.DecodeRuneInString(text[prefix:]); r != s || size != n {
			break
		}
		prefix += size
	}
	old, text = old[prefix:], text[prefix:]
	suffix := 0
	for suffix < len(old) && suffix < len(text) {
		r, size := utf8.DecodeLastRuneInString(old[:len(old)-suffix])
		if s, n := utf8.DecodeLastRuneInString(text[:len(text)-suffix]); r != s || size != n {
			break
		}
		suffix += size
	}
	return e.Start + prefix, e.End - suffix, text[:len(text)-suffix]
}

func (m *Map) Edits(edits []textedit.Edit) ([]textedit.Edit, bool) {
	out := make([]textedit.Edit, len(edits))
	for i, e := range edits {
		mapped, ok := m.Edit(e)
		if !ok {
			return nil, false
		}
		out[i] = mapped
	}
	return out, true
}
