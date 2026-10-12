package lint

import (
	"bytes"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var bracesFamily = family{
	codes: map[Code]meta{
		2051: {severity: SeverityWarning, summary: "Braces are expanded before variables, so a range built from variables is not a range."},
		2175: {severity: SeverityStyle, summary: "A brace range given to eval unquoted is expanded before the variable is substituted."},
		3009: {shells: strictShells, summary: "Brace expansion is undefined in POSIX sh."},
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
	elements [][]byte
}

func (g braceGroup) expands() bool {
	return len(g.elements) > 1 || bytes.Contains(g.elements[0], []byte(".."))
}

func (g braceGroup) rangesOverExpansion() bool {
	return slices.ContainsFunc(g.elements, func(e []byte) bool {
		return bytes.Contains(e, []byte("..$")) || bytes.Contains(e, []byte("$.."))
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
		p.nonPortable(portabilityIssue{code: 3009, feature: "Brace expansion " + p.excerpt(g.span), alternative: "a loop or separate words"}, g.span)
		if p.dialect == dialect.Bash && g.rangesOverExpansion() {
			p.reportBraceRangeVariable(g)
		}
	}
}

func (p *pass) reportBraceRangeVariable(g braceGroup) {
	text := p.excerpt(g.span)
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
			elements := make([][]byte, len(bounds)-1)
			for j := range elements {
				elements[j] = rendered[bounds[j]+1 : bounds[j+1]]
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
