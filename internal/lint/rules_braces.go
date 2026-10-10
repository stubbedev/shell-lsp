package lint

import (
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

var bracesFamily = family{
	codes: map[Code]meta{
		2051: {severity: SeverityWarning},
		2175: {severity: SeverityStyle},
		3009: {severity: SeverityWarning},
	},
	rules: []rule{{on: nodes(&syntax.Word{}), node: checkBraceExpansions}},
}

const (
	expansionMarker = '$'
	opaqueMarker    = '-'
	escapedMarker   = '_'
)

type braceGroup struct {
	span     parser.Span
	elements []string
}

func (g braceGroup) expands() bool {
	return len(g.elements) > 1 || strings.Contains(g.elements[0], "..")
}

func (g braceGroup) rangesOverExpansion() bool {
	return slices.ContainsFunc(g.elements, func(e string) bool {
		return strings.Contains(e, "..$") || strings.Contains(e, "$..")
	})
}

func checkBraceExpansions(p *pass, n syntax.Node) {
	w, ok := n.(*syntax.Word)
	if !ok || (p.dialect != dialect.Bash && !strictShells.Has(p.dialect)) || !hasBraceCandidate(w) || !p.parsesGlobs(w) {
		return
	}
	for _, g := range braceGroups(w) {
		if !g.expands() {
			continue
		}
		p.nonPortable(portabilityIssue{code: 3009, shells: strictShells, feature: "Brace expansion " + p.text[g.span.Start:g.span.End], alternative: "a loop or separate words"}, g.span)
		if p.dialect == dialect.Bash && g.rangesOverExpansion() {
			p.reportBraceRangeVariable(g)
		}
	}
}

func (p *pass) reportBraceRangeVariable(g braceGroup) {
	text := p.text[g.span.Start:g.span.End]
	if call, ok := p.parent().(*syntax.CallExpr); ok {
		if name, _ := parser.Literal(call.Args[0]); name == "eval" {
			p.report(2175, g.span, "Quote "+text+" so eval receives it literally and expands the range after the variable is substituted.")
			return
		}
	}
	p.report(2051, g.span, "Bash expands braces before variables, so "+text+" is not a range; use a for ((...)) loop or seq instead.")
}

func hasBraceCandidate(w *syntax.Word) bool {
	return slices.ContainsFunc(w.Parts, func(part syntax.WordPart) bool {
		lit, ok := part.(*syntax.Lit)
		return ok && strings.IndexByte(lit.Value, '{') >= 0
	})
}

func braceGroups(w *syntax.Word) []braceGroup {
	var rendered []byte
	var offsets []int
	for _, part := range w.Parts {
		lit, ok := part.(*syntax.Lit)
		if !ok {
			rendered = append(rendered, partMarker(part))
			offsets = append(offsets, -1)
			continue
		}
		start := parser.Offset(lit.ValuePos)
		for i := 0; i < len(lit.Value); i++ {
			c := lit.Value[i]
			if c == '\\' && i+1 < len(lit.Value) {
				rendered = append(rendered, escapedMarker, escapedMarker)
				offsets = append(offsets, start+i, start+i+1)
				i++
				continue
			}
			rendered = append(rendered, c)
			offsets = append(offsets, start+i)
		}
	}
	type frame struct {
		open   int
		commas []int
	}
	var stack []frame
	var groups []braceGroup
	for i, c := range rendered {
		switch {
		case c == '{':
			stack = append(stack, frame{open: i})
		case c == ',' && len(stack) > 0:
			top := &stack[len(stack)-1]
			top.commas = append(top.commas, i)
		case c == '}' && len(stack) > 0:
			f := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			bounds := append(append([]int{f.open}, f.commas...), i)
			elements := make([]string, len(bounds)-1)
			for j := range elements {
				elements[j] = string(rendered[bounds[j]+1 : bounds[j+1]])
			}
			groups = append(groups, braceGroup{span: parser.Span{Start: offsets[f.open], End: offsets[i] + 1}, elements: elements})
		}
	}
	return groups
}

func partMarker(part syntax.WordPart) byte {
	switch part.(type) {
	case *syntax.ParamExp, *syntax.CmdSubst, *syntax.ArithmExp:
		return expansionMarker
	}
	return opaqueMarker
}
