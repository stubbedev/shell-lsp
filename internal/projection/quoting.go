package projection

import (
	"cmp"
	"slices"
	"sort"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type singleQuote struct {
	start, end ProjOffset
	dollar     bool
}

func (q singleQuote) closing() string {
	if q.dollar {
		return "'" + Dynamic + "$'"
	}
	return "'" + Dynamic + "'"
}

type requote struct {
	span int
	text string
	kind kind
}

func (m *Map) settle(placeholders []int, programs []program) {
	var requotes []requote
	parsers := map[dialect.Shell]parser.SyntaxParser{}
	next, nextProgram := 0, 0
	for i, script := range m.scripts {
		first, firstProgram := next, nextProgram
		for next < len(placeholders) && m.spans[placeholders[next]].proj < ProjOffset(script.End) {
			next++
		}
		for nextProgram < len(programs) && m.spans[programs[nextProgram].hole].proj < ProjOffset(script.End) {
			nextProgram++
		}
		text := m.text[script.Start:script.End]
		quoted := first != next && strings.Contains(text, "'")
		named := programs[firstProgram:nextProgram]
		if !quoted && len(named) == 0 {
			continue
		}
		shell := m.shells[i].Shell
		p, ok := parsers[shell]
		if !ok {
			p = parser.NewSyntaxParser(shell)
			parsers[shell] = p
		}
		file := p.Parse(text)
		if quoted {
			quotes := singleQuotes(file, ProjOffset(script.Start))
			for _, at := range placeholders[first:next] {
				if q, ok := enclosing(quotes, m.spans[at].proj); ok {
					requotes = append(requotes, requote{span: at, text: q.closing()})
				}
			}
		}
		if len(named) > 0 {
			requotes = append(requotes, m.commandWords(file, ProjOffset(script.Start), named)...)
		}
	}
	if len(requotes) > 0 {
		slices.SortFunc(requotes, func(a, b requote) int { return cmp.Compare(a.span, b.span) })
		m.apply(requotes)
	}
}

func singleQuotes(file *syntax.File, base ProjOffset) []singleQuote {
	var quotes []singleQuote
	parser.Walk(file, func(n syntax.Node) bool {
		q, ok := n.(*syntax.SglQuoted)
		if !ok {
			return true
		}
		opening := ProjOffset(len("'"))
		if q.Dollar {
			opening += ProjOffset(len("$"))
		}
		quotes = append(quotes, singleQuote{start: base + ProjOffset(parser.Offset(q.Left)) + opening, end: base + ProjOffset(parser.Offset(q.Right)), dollar: q.Dollar})
		return false
	})
	slices.SortFunc(quotes, func(a, b singleQuote) int { return cmp.Compare(a.start, b.start) })
	return quotes
}

func enclosing(quotes []singleQuote, at ProjOffset) (singleQuote, bool) {
	i := sort.Search(len(quotes), func(i int) bool { return quotes[i].end > at })
	if i == len(quotes) || quotes[i].start > at {
		return singleQuote{}, false
	}
	return quotes[i], true
}

func (m *Map) apply(requotes []requote) {
	origins := make([]ProjOffset, len(requotes))
	shifts := make([]int, len(requotes)+1)
	for i, r := range requotes {
		origins[i] = m.spans[r.span].proj
		shifts[i+1] = shifts[i] + len(r.text) - m.spans[r.span].projLen
	}
	moved := func(at ProjOffset) ProjOffset {
		before, _ := slices.BinarySearch(origins, at)
		return at + ProjOffset(shifts[before])
	}
	var text strings.Builder
	text.Grow(len(m.text) + shifts[len(requotes)])
	copied := 0
	for i, r := range requotes {
		text.WriteString(m.text[copied:origins[i]])
		text.WriteString(r.text)
		copied = int(origins[i]) + m.spans[r.span].projLen
	}
	text.WriteString(m.text[copied:])
	spans := slices.Clone(m.spans)
	for i := range spans {
		spans[i].proj = moved(spans[i].proj)
	}
	for _, r := range requotes {
		spans[r.span].projLen = len(r.text)
		if r.kind != 0 {
			spans[r.span].kind = r.kind
		}
	}
	for i, s := range m.scripts {
		m.scripts[i] = parser.Span{Start: int(moved(ProjOffset(s.Start))), End: int(moved(ProjOffset(s.End)))}
	}
	m.text, m.spans = text.String(), spans
}
