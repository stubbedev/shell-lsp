package projection

import "sort"

func (b *Builder) StandIn(text string) { b.standIn = text }

func (b *Builder) Embed(inner, via *Map) {
	region := noRegion
	next := 0
	for _, s := range inner.spans {
		for next < len(inner.scripts) && int(s.proj) >= inner.scripts[next].Start {
			b.Shell(inner.shells[next])
			b.Script()
			next++
		}
		if next == 0 {
			continue
		}
		if s.region != noRegion && s.region != region {
			region = s.region
			b.Enter(composed(inner, via, region))
		}
		b.embedded(inner, via, s)
	}
}

func composed(inner, via *Map, region int) *Region {
	return &Region{encode: func(host string, start, end int, text string, set *editSet) (string, bool) {
		from, _ := via.Offset(HostOffset(start))
		to, _ := via.Offset(HostOffset(end))
		encoded, ok := inner.regions[region].encode(inner.host, int(from), int(to), text, set)
		if !ok {
			return "", false
		}
		return via.regions[len(via.regions)-1].encode(host, start, end, encoded, set)
	}}
}

func (b *Builder) embedded(inner, via *Map, s span) {
	text := inner.text[s.proj:s.projEnd()]
	start := max(via.startOf(int(s.host)), b.at)
	end := max(via.endOf(int(s.host), int(s.hostEnd())), start)
	if s.kind != opaque {
		b.outerIndent(via, start)
	}
	if s.kind != verbatim {
		switch s.kind {
		case escape:
			b.Escape(start, end, text)
		case indent:
			b.Indent(start, end)
		default:
			b.Opaque(start, end, text)
		}
		return
	}
	first := sort.Search(len(via.spans), func(i int) bool { return via.spans[i].projEnd() > ProjOffset(s.host) })
	for _, part := range via.spans[first:] {
		if int(part.proj) >= int(s.hostEnd()) {
			break
		}
		if part.projLen == 0 {
			if part.kind == indent {
				b.outerIndent(via, int(part.hostEnd()))
			}
			continue
		}
		b.throughDecoded(via, part, max(int(s.host), int(part.proj)), min(int(s.hostEnd()), int(part.projEnd())))
	}
}

func (b *Builder) throughDecoded(via *Map, part span, from, to int) {
	if int(part.host) < b.at && part.kind != verbatim {
		return
	}
	switch part.kind {
	case verbatim:
		if lo, hi := max(int(part.host)+from-int(part.proj), b.at), int(part.host)+to-int(part.proj); lo < hi {
			b.Verbatim(lo, hi)
		}
	case escape:
		b.Escape(int(part.host), int(part.hostEnd()), via.text[part.proj:part.projEnd()])
	default:
		b.Placeholder(int(part.host), int(part.hostEnd()))
	}
}

func (b *Builder) outerIndent(via *Map, end int) {
	i := sort.Search(len(via.spans), func(i int) bool { return via.spans[i].hostEnd() >= HostOffset(end) })
	for ; i < len(via.spans) && via.spans[i].hostEnd() == HostOffset(end); i++ {
		if s := via.spans[i]; s.kind == indent && int(s.host) >= b.at && s.hostLen > 0 {
			b.Indent(int(s.host), end)
			return
		}
	}
}

func Through(inner, via *Map) *Map {
	b := NewBuilder(via.host)
	b.StandIn(inner.standIn)
	b.Embed(inner, via)
	return b.Map()
}

func (m *Map) startOf(proj int) int { return int(m.HostOffset(ProjOffset(proj))) }

func (m *Map) endOf(start, end int) int {
	if end <= start {
		return m.startOf(start)
	}
	i := sort.Search(len(m.spans), func(i int) bool { return m.spans[i].projEnd() >= ProjOffset(end) })
	if i == len(m.spans) {
		return m.startOf(end)
	}
	s := m.spans[i]
	if s.kind == verbatim {
		return int(s.host) + end - int(s.proj)
	}
	return int(s.hostEnd())
}

func (m *Map) HostSpan(start, end int) (int, int) { return m.startOf(start), m.endOf(start, end) }

func (m *Map) TouchesStandIn(start, end int) bool {
	i := sort.Search(len(m.spans), func(i int) bool { return int(m.spans[i].projEnd()) >= start })
	for ; i < len(m.spans) && int(m.spans[i].proj) <= end; i++ {
		if s := m.spans[i]; (s.kind == opaque || s.kind == standIn) && s.projLen > 0 {
			return true
		}
	}
	return false
}
