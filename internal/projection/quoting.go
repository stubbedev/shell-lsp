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
	shift, line, counted := 0, 1, 0
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
		line += strings.Count(m.text[counted:script.Start], "\n")
		counted = script.Start
		file, base := m.settledParse(p, shell, text, parser.FragmentStart{Offset: script.Start, Line: line}, shift)
		before := len(requotes)
		if quoted {
			quotes := singleQuotes(file, base)
			for _, at := range placeholders[first:next] {
				if q, ok := enclosing(quotes, m.spans[at].proj); ok {
					requotes = append(requotes, requote{span: at, text: q.closing()})
				}
			}
		}
		if len(named) > 0 {
			requotes = append(requotes, m.commandWords(file, base, named)...)
		}
		for _, r := range requotes[before:] {
			shift += len(r.text) - m.spans[r.span].projLen
		}
	}
	if len(requotes) > 0 {
		slices.SortFunc(requotes, func(a, b requote) int { return cmp.Compare(a.span, b.span) })
		m.apply(requotes)
	}
}

func (m *Map) settledParse(p parser.SyntaxParser, shell dialect.Shell, text string, at parser.FragmentStart, shift int) (*syntax.File, ProjOffset) {
	if at.Offset != 0 && m.text[at.Offset-1] != '\n' {
		return parser.NewSyntaxParser(shell).Parse(text), ProjOffset(at.Offset)
	}
	key := parser.ParseKey{Text: text, Start: at.Offset + shift, Line: at.Line, Column: 1, Shell: shell}
	if file, ready := m.Recall(key); ready {
		return file, ProjOffset(-shift)
	}
	if file, clean := p.ParseFragment(text, parser.FragmentStart{Offset: key.Start, Line: at.Line}); clean {
		m.Keep(key, file)
		return file, ProjOffset(-shift)
	}
	return parser.NewSyntaxParser(shell).Parse(text), ProjOffset(at.Offset)
}

func (m *Map) Recall(key parser.ParseKey) (*syntax.File, bool) {
	if file, kept := m.parses[key]; kept {
		return file, true
	}
	if m.earlier == nil {
		return nil, false
	}
	file, kept := m.earlier.parses[key]
	if kept {
		m.Keep(key, file)
	}
	return file, kept
}

func (m *Map) Keep(key parser.ParseKey, file *syntax.File) {
	if m.parses == nil {
		m.parses = map[parser.ParseKey]*syntax.File{}
	}
	m.parses[key] = file
}

func (m *Map) Reuse(earlier *Map) { m.earlier = earlier }

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
