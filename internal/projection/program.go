package projection

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const noSpan = -1

type program struct {
	hole, path, base int
	name             string
}

func (b *Builder) Program(start, end, pathEnd, baseEnd int) {
	b.Placeholder(start, end)
	hole := len(b.spans) - 1
	b.sealed = true
	b.Verbatim(end, pathEnd)
	b.sealed = true
	b.Verbatim(pathEnd, baseEnd)
	b.sealed = true
	b.programs = append(b.programs, program{hole: hole, path: hole + 1, base: hole + 2, name: b.host[pathEnd:baseEnd]})
}

func (b *Builder) NamedPlaceholder(start, end int, name string) {
	b.Placeholder(start, end)
	b.programs = append(b.programs, program{hole: len(b.spans) - 1, path: noSpan, base: noSpan, name: name})
}

func (m *Map) namedWord(p program, w *syntax.Word, base ProjOffset) bool {
	start, end := base+ProjOffset(parser.Offset(w.Pos())), base+ProjOffset(parser.Offset(w.End()))
	hole := m.spans[p.hole].proj
	last := m.spans[p.hole].projEnd()
	if p.base != noSpan {
		last = m.spans[p.base].projEnd()
	}
	if start == hole {
		return end == last
	}
	return start+1 == hole && m.text[start] == '"' && end == last+1
}

func (m *Map) wordProgram(w *syntax.Word, base ProjOffset, named []program) (program, bool) {
	for _, p := range named {
		if m.namedWord(p, w, base) {
			return p, true
		}
	}
	return program{}, false
}

func (m *Map) commandWords(file *syntax.File, base ProjOffset, named []program) []requote {
	wrappers := parser.Wrappers{Programs: true}
	done := map[int]bool{}
	var out []requote
	parser.Walk(file, func(n syntax.Node) bool {
		call, ok := n.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		for run := range wrappers.Invocations(call.Args) {
			for _, w := range run.Args {
				p, found := m.wordProgram(w, base, named)
				if !found {
					if lit, literal := parser.Literal(w); !literal || !strings.HasPrefix(lit, "-") {
						break
					}
					continue
				}
				if !done[p.hole] {
					done[p.hole] = true
					out = append(out, m.named(p)...)
				}
				if !wrappers.Has(p.name) {
					break
				}
			}
		}
		return true
	})
	return out
}

func (m *Map) named(p program) []requote {
	if p.base == noSpan {
		return []requote{{span: p.hole, text: p.name}}
	}
	return []requote{
		{span: p.hole, kind: hidden},
		{span: p.path, kind: elided},
		{span: p.base, text: p.name, kind: pinned},
	}
}
