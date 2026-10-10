package lint

import (
	"slices"
	"strings"
	"unicode/utf8"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/parser"
)

var charactersFamily = family{
	codes: map[Code]meta{
		1017: {severity: SeverityError},
		1018: {severity: SeverityError},
	},
	rules: []rule{
		{node: checkUnicodeSpaces},
		{finish: checkCarriageReturns},
	},
}

const noBreakSpace = ' '

func isUnicodeSpace(r rune) bool {
	return r == noBreakSpace || ' ' <= r && r <= ' ' || r == '​' || r == ' '
}

func checkUnicodeSpaces(p *pass, n syntax.Node) {
	lit, ok := n.(*syntax.Lit)
	if !ok {
		return
	}
	word, ok := p.enclosingShellWord()
	if !ok {
		return
	}
	span, ok := p.span(lit)
	if !ok {
		return
	}
	tokenStart := word.Parts[0] == lit
	for i, r := range p.text[span.Start:span.End] {
		tokenStart = r == noBreakSpace || tokenStart && isUnicodeSpace(r)
		if !tokenStart {
			continue
		}
		at := span.Start + i
		p.report(1018, parser.Span{Start: at, End: at + utf8.RuneLen(r)}, "This is a Unicode space, not a plain one, so the shell does not split words here; delete it and type a regular space.")
	}
}

func checkCarriageReturns(p *pass) {
	if !strings.Contains(p.text, "\r") {
		return
	}
	verbatim := p.verbatimRegions()
	next := 0
	for i := range len(p.text) {
		if p.text[i] != '\r' || i > 0 && p.text[i-1] == '\\' {
			continue
		}
		for next < len(verbatim) && verbatim[next].End <= i {
			next++
		}
		if next < len(verbatim) && verbatim[next].Start <= i {
			continue
		}
		p.report(1017, parser.Span{Start: i, End: i + 1}, "Literal carriage return, typically from Windows line endings; convert the file to Unix line endings (for example with dos2unix).")
	}
}

func (p *pass) verbatimRegions() []parser.Span {
	var out []parser.Span
	syntax.Walk(p.tree.File, func(n syntax.Node) bool {
		switch n.(type) {
		case *syntax.SglQuoted, *syntax.DblQuoted:
			if span, ok := p.span(n); ok {
				out = append(out, span)
			}
			return false
		}
		return true
	})
	for _, h := range p.tree.Heredocs {
		start := h.Body.Start
		if h.Body.End == h.Body.Start {
			start = lineEnd(p.text, h.Span.End) + 1
		}
		out = append(out, parser.Span{Start: start, End: lineEnd(p.text, max(start, h.Body.End))})
	}
	slices.SortFunc(out, func(a, b parser.Span) int { return a.Start - b.Start })
	return out
}

func lineEnd(text string, from int) int {
	from = min(from, len(text))
	if i := strings.IndexByte(text[from:], '\n'); i >= 0 {
		return from + i
	}
	return len(text)
}
