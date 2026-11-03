package projection

import (
	"cmp"
	"encoding/binary"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type kind uint8

const (
	verbatim kind = iota + 1
	escape
	opaque
	hidden
	elided
	pinned
	standIn
	indent
)

type Encoder func(host string, start, end int, text string) (string, bool)

type Settled func(host string, edits []textedit.Edit) bool

type Region struct {
	encode  func(host string, start, end int, text string, set *editSet) (string, bool)
	settled Settled
}

func NewRegion(encode Encoder, settled Settled) *Region {
	r := &Region{settled: settled}
	r.encode = func(host string, start, end int, text string, set *editSet) (string, bool) {
		encoded, ok := encode(host, start, end, text)
		if ok {
			set.record(r, host, textedit.Edit{Start: start, End: end, NewText: encoded})
		}
		return encoded, ok
	}
	return r
}

type regionEdits struct {
	region *Region
	host   string
	edits  []textedit.Edit
}

type editSet struct {
	regions []regionEdits
}

func (set *editSet) record(r *Region, host string, e textedit.Edit) {
	if r.settled == nil {
		return
	}
	i := slices.IndexFunc(set.regions, func(have regionEdits) bool { return have.region == r })
	if i < 0 {
		i = len(set.regions)
		set.regions = append(set.regions, regionEdits{region: r, host: host})
	}
	set.regions[i].edits = append(set.regions[i].edits, e)
}

func (set *editSet) holds() bool {
	return !slices.ContainsFunc(set.regions, func(have regionEdits) bool { return !have.region.settled(have.host, have.edits) })
}

type scriptReads struct {
	start int
	names map[string]bool
}

type HostOffset int

type ProjOffset int

type span struct {
	kind    kind
	region  int
	proj    ProjOffset
	projLen int
	host    HostOffset
	hostLen int
}

func (s span) projEnd() ProjOffset { return s.proj + ProjOffset(s.projLen) }

func (s span) hostEnd() HostOffset { return s.host + HostOffset(s.hostLen) }

func (s span) editable() bool { return s.kind == verbatim || s.kind == escape }

type Map struct {
	host    string
	text    string
	spans   []span
	regions []*Region
	scripts []parser.Span
	shells  []dialect.Detection
	sourced []int
	units   []int
	reads   []scriptReads
	parses  map[parser.ParseKey]*syntax.File
	earlier *Map
	standIn string
}

func (m *Map) Host() string { return m.host }

func (m *Map) Text() string { return m.text }

const (
	noRegion   = -1
	SharedUnit = 0
)

type Builder struct {
	host         string
	text         strings.Builder
	spans        []span
	regions      []*Region
	starts       []int
	shells       []dialect.Detection
	shell        dialect.Detection
	at           int
	placeholders []int
	programs     []program
	sealed       bool
	standIn      string
	sourced      []int
	units        []int
	reads        []scriptReads
	lastUnit     int
	earlier      *Map
}

const (
	Dynamic      = "$$"
	UnknownValue = "$_"
)

func NewBuilder(host string) *Builder {
	return &Builder{host: host}
}

func (b *Builder) Region(encode Encoder) { b.Enter(NewRegion(encode, nil)) }

func (b *Builder) Enter(r *Region) { b.regions = append(b.regions, r) }

func (b *Builder) Shell(d dialect.Detection) { b.shell = d }

func (b *Builder) Reserve(spans int) { b.spans = slices.Grow(b.spans, spans) }

func (b *Builder) Reuse(earlier *Map) { b.earlier = earlier }

func (b *Builder) Script() { b.UnitScript(SharedUnit) }

func (b *Builder) ReadByHost(names map[string]bool) {
	b.reads = append(b.reads, scriptReads{start: b.text.Len(), names: names})
}

func (b *Builder) UnitScript(unit int) {
	b.starts = append(b.starts, b.text.Len())
	b.shells = append(b.shells, b.shell)
	b.units = append(b.units, unit)
}

func (b *Builder) NewUnit() int {
	b.lastUnit++
	return b.lastUnit
}

func (b *Builder) SourcedScript() {
	b.sourced = append(b.sourced, b.text.Len())
	b.Script()
}

func (b *Builder) Placeholder(start, end int) {
	b.placeholders = append(b.placeholders, len(b.spans))
	b.add(standIn, start, end, cmp.Or(b.standIn, Dynamic))
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

func (b *Builder) Indent(start, end int) {
	b.add(indent, start, end, "")
}

func (b *Builder) add(k kind, start, end int, text string) {
	if start < b.at || end < start || end > len(b.host) {
		panic(fmt.Sprintf("projection: span %d-%d is out of order after host offset %d of %d", start, end, b.at, len(b.host)))
	}
	region := len(b.regions) - 1
	if k != opaque && k != standIn && region == noRegion {
		panic(fmt.Sprintf("projection: editable span %d-%d precedes the first region", start, end))
	}
	s := span{kind: k, region: region, proj: ProjOffset(b.text.Len()), projLen: len(text), host: HostOffset(start), hostLen: end - start}
	b.text.WriteString(text)
	b.at = end
	sealed := b.sealed
	b.sealed = false
	if n := len(b.spans); n > 0 && (k == verbatim || k == indent) && !sealed {
		last := &b.spans[n-1]
		if last.kind == k && last.region == region && last.hostEnd() == HostOffset(start) {
			last.projLen += s.projLen
			last.hostLen += s.hostLen
			return
		}
	}
	b.spans = append(b.spans, s)
}

func (b *Builder) Map() *Map {
	scripts, shells, units := b.scripts()
	if cap(b.spans) > 2*len(b.spans) {
		b.spans = slices.Clone(b.spans)
	}
	m := &Map{host: b.host, text: b.text.String(), spans: b.spans, regions: b.regions, scripts: scripts, shells: shells, sourced: b.sourcedScripts(scripts), units: units, reads: b.reads, standIn: b.standIn, earlier: b.earlier}
	if b.standIn == "" {
		m.settle(b.placeholders, b.programs)
	}
	return m
}

func (b *Builder) scripts() ([]parser.Span, []dialect.Detection, []int) {
	starts, shells, units := append([]int{0}, b.starts...), append([]dialect.Detection{b.shell}, b.shells...), append([]int{SharedUnit}, b.units...)
	if len(b.shells) > 0 {
		shells[0] = b.shells[0]
	}
	var spans []parser.Span
	var detections []dialect.Detection
	var kept []int
	for i, start := range starts {
		end := b.text.Len()
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		if start == end {
			continue
		}
		spans = append(spans, parser.Span{Start: start, End: end})
		detections = append(detections, shells[i])
		kept = append(kept, units[i])
	}
	return spans, detections, kept
}

func (b *Builder) sourcedScripts(scripts []parser.Span) []int {
	if len(b.sourced) == 0 {
		return nil
	}
	var out []int
	for i, script := range scripts {
		if _, sourced := slices.BinarySearch(b.sourced, script.Start); sourced {
			out = append(out, i)
		}
	}
	return out
}

func (m *Map) Scripts() []parser.Span { return m.scripts }

func (m *Map) Sourced(offset ProjOffset) bool {
	_, sourced := slices.BinarySearch(m.sourced, m.ScriptOf(offset))
	return sourced
}

func (m *Map) ReadByHost(offset ProjOffset, name string) bool {
	script := m.ScriptOf(offset)
	if script == len(m.scripts) {
		return false
	}
	i, marked := slices.BinarySearchFunc(m.reads, m.scripts[script].Start, func(r scriptReads, start int) int { return cmp.Compare(r.start, start) })
	return marked && m.reads[i].names[name]
}

func (m *Map) ScriptOf(offset ProjOffset) int {
	return sort.Search(len(m.scripts), func(i int) bool { return m.scripts[i].End > int(offset) })
}

func (m *Map) UnitOf(script int) int { return m.units[script] }

func (m *Map) ShellOf(offset ProjOffset) int {
	script := m.ScriptOf(offset)
	if script < len(m.units) && m.units[script] != SharedUnit {
		return m.units[script]
	}
	return -1 - script
}

func (m *Map) AppendLayout(dst []byte, script parser.Span) []byte {
	first := sort.Search(len(m.spans), func(i int) bool { return m.spans[i].projEnd() > ProjOffset(script.Start) })
	for i := first; i < len(m.spans) && int(m.spans[i].proj) < script.End; i++ {
		s := m.spans[i]
		dst = append(dst, byte(s.kind))
		if s.hostLen == 0 {
			dst = append(dst, 0)
		}
		dst = binary.AppendVarint(dst, int64(s.projLen))
	}
	return dst
}

func (m *Map) Shells() []dialect.Detection { return m.shells }

func (m *Map) Synthetic(offset ProjOffset) bool {
	i := sort.Search(len(m.spans), func(i int) bool { return m.spans[i].projEnd() > offset })
	return i < len(m.spans) && m.spans[i].hostLen == 0
}

func (m *Map) StandIn(offset ProjOffset) bool {
	i := sort.Search(len(m.spans), func(i int) bool { return m.spans[i].projEnd() > offset })
	return i < len(m.spans) && m.spans[i].kind == standIn
}

func (m *Map) OnlyStandIn(start, end ProjOffset) bool {
	found := false
	for i := sort.Search(len(m.spans), func(i int) bool { return m.spans[i].projEnd() > start }); i < len(m.spans) && m.spans[i].proj < end; i++ {
		s := m.spans[i]
		switch {
		case s.projLen == 0:
		case s.kind == standIn:
			found = true
		case strings.Trim(m.text[max(s.proj, start):min(s.projEnd(), end)], `"'`) != "":
			return false
		}
	}
	return found
}

func (m *Map) HostOffset(offset ProjOffset) HostOffset {
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
	if s.kind == verbatim || s.kind == pinned {
		return s.host + HostOffset(offset-s.proj)
	}
	return s.host
}

func (m *Map) Offset(host HostOffset) (ProjOffset, bool) {
	i := sort.Search(len(m.spans), func(i int) bool { return m.spans[i].hostEnd() > host })
	within := i < len(m.spans) && m.spans[i].host <= host
	if within && m.spans[i].kind == elided {
		return m.spans[i].proj, true
	}
	if within && m.spans[i].kind == hidden {
		return m.spans[i].proj, false
	}
	if within && m.spans[i].projLen > 0 {
		s := m.spans[i]
		if s.kind == verbatim || s.kind == pinned {
			return s.proj + ProjOffset(host-s.host), true
		}
		return s.proj, s.kind == escape
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
	return ProjOffset(len(m.text)), false
}

func (m *Map) Edit(e textedit.Edit) (textedit.Edit, bool) {
	var set editSet
	mapped, ok := m.edit(e, &set)
	return mapped, ok && set.holds()
}

func (m *Map) edit(e textedit.Edit, set *editSet) (textedit.Edit, bool) {
	if m.writesStandIn(e) {
		return textedit.Edit{}, false
	}
	start, end, text := m.trim(e)
	return m.replace(textedit.Edit{Start: start, End: end, NewText: text}, set)
}

func (m *Map) writesStandIn(e textedit.Edit) bool {
	return m.standIn != "" && strings.Count(e.NewText, m.standIn) > strings.Count(m.text[e.Start:e.End], m.standIn)
}

func (m *Map) Replacement(e textedit.Edit) (textedit.Edit, bool) {
	var set editSet
	mapped, ok := m.replace(e, &set)
	return mapped, ok && set.holds()
}

func (m *Map) replace(e textedit.Edit, set *editSet) (textedit.Edit, bool) {
	start, end, text := ProjOffset(e.Start), ProjOffset(e.End), e.NewText
	if start == end {
		return m.insert(start, text, set)
	}
	first := sort.Search(len(m.spans), func(i int) bool { return m.spans[i].projEnd() > start })
	var covered []span
	following := len(m.spans)
	for i, s := range m.spans[first:] {
		if s.proj >= end {
			following = first + i
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
		if !s.editable() && s.kind != indent || s.region != covered[0].region {
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
		hostStart += HostOffset(start - firstSpan.proj)
	}
	if lastSpan.kind == verbatim {
		hostEnd = lastSpan.host + HostOffset(end-lastSpan.proj)
	}
	if following < len(m.spans) && lastSpan.kind == verbatim && m.spans[following].kind == indent && m.spans[following].proj == end {
		if own, whole := m.ownIndent(first, firstSpan, start); whole && text == "" {
			hostStart = own
		} else {
			hostEnd = m.spans[following].hostEnd()
		}
	}
	return m.encode(firstSpan.region, hostStart, hostEnd, text, set)
}

func (m *Map) ownIndent(first int, s span, start ProjOffset) (HostOffset, bool) {
	if first == 0 || s.kind != verbatim || s.proj != start || m.spans[first-1].kind != indent {
		return 0, false
	}
	return m.spans[first-1].host, true
}

func (m *Map) insert(offset ProjOffset, text string, set *editSet) (textedit.Edit, bool) {
	var at, past *span
	for i := sort.Search(len(m.spans), func(i int) bool { return m.spans[i].projEnd() >= offset }); i < len(m.spans); i++ {
		s := &m.spans[i]
		if s.proj > offset {
			break
		}
		if s.kind == indent && at != nil && at.hostEnd() == s.host && (!strings.HasSuffix(text, "\n") || m.lineHolds(i+1, at.region)) {
			past = s
		}
		if !s.editable() || (s.kind == escape && offset != s.proj && offset != s.projEnd()) {
			continue
		}
		at, past = s, nil
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
		host = at.host + HostOffset(offset-at.proj)
	case offset == at.proj:
		host = at.host
	}
	if past != nil {
		host = past.hostEnd()
	}
	return m.encode(at.region, host, host, text, set)
}

func (m *Map) lineHolds(i, region int) bool {
	return i < len(m.spans) && m.spans[i].region == region && (m.spans[i].editable() || m.spans[i].kind == standIn)
}

func (m *Map) encode(region int, start, end HostOffset, text string, set *editSet) (textedit.Edit, bool) {
	encoded, ok := m.regions[region].encode(m.host, int(start), int(end), text, set)
	if !ok {
		return textedit.Edit{}, false
	}
	return textedit.Edit{Start: int(start), End: int(end), NewText: encoded}, true
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
	var set editSet
	for i, e := range edits {
		mapped, ok := m.edit(e, &set)
		if !ok {
			return nil, false
		}
		out[i] = mapped
	}
	if !set.holds() {
		return nil, false
	}
	return out, true
}
