package lint

import (
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

var caseFamily = family{
	codes: map[Code]meta{
		2194: {severity: SeverityWarning},
		2195: {severity: SeverityWarning},
		2221: {severity: SeverityWarning},
		2222: {severity: SeverityWarning},
	},
	rules: []rule{{on: nodes(&syntax.CaseClause{}), node: checkCaseClause}},
}

type globToken struct {
	kind globKind
	char byte
}

type globKind uint8

const (
	globChar globKind = iota
	globOne
	globAny
)

type globPattern []globToken

func checkCaseClause(p *pass, n syntax.Node) {
	c, ok := n.(*syntax.CaseClause)
	if !ok || c.Word == nil {
		return
	}
	if constantWord(c.Word) {
		if span, ok := p.span(c.Word); ok {
			p.report(2194, span, "The case word is a constant, so the same branch is taken every time; did you mean to use a variable?")
		}
	} else {
		p.checkImpossiblePatterns(c)
	}
	p.checkShadowedPatterns(c)
}

func constantWord(w *syntax.Word) bool {
	if first, ok := w.Parts[0].(*syntax.Lit); ok && strings.HasPrefix(first.Value, "~") {
		return false
	}
	for _, part := range w.Parts {
		switch part := part.(type) {
		case *syntax.Lit, *syntax.SglQuoted:
		case *syntax.DblQuoted:
			for _, inner := range part.Parts {
				if _, ok := inner.(*syntax.Lit); !ok {
					return false
				}
			}
		default:
			return false
		}
	}
	return true
}

func (p *pass) checkImpossiblePatterns(c *syntax.CaseClause) {
	word := globOf(c.Word)
	for _, item := range c.Items {
		for _, pattern := range item.Patterns {
			if overlaps(word, globOf(pattern)) {
				continue
			}
			if span, ok := p.span(pattern); ok {
				p.report(2195, span, "This pattern can never match the case word, so the branch is dead code.")
			}
		}
	}
}

func (p *pass) checkShadowedPatterns(c *syntax.CaseClause) {
	var patterns []*syntax.Word
	for _, item := range c.Items {
		if item.Op == syntax.Break || item.Op == 0 {
			patterns = append(patterns, item.Patterns...)
		}
	}
	fuzzy := make([]globPattern, len(patterns))
	for i, pattern := range patterns {
		fuzzy[i] = globOf(pattern)
	}
	for i, pattern := range patterns {
		exact, ok := exactGlob(pattern)
		if !ok {
			continue
		}
		for j := i + 1; j < len(patterns); j++ {
			if !covers(exact, fuzzy[j]) {
				continue
			}
			p.reportShadowed(pattern, patterns[j])
			break
		}
	}
}

func (p *pass) reportShadowed(earlier, later *syntax.Word) {
	first, ok := p.span(earlier)
	if !ok {
		return
	}
	second, ok := p.span(later)
	if !ok {
		return
	}
	p.report(2221, first, "This pattern also matches everything the pattern on line "+strconv.Itoa(p.tree.Line(second.Start)+1)+" matches, so that one is never reached.")
	p.report(2222, second, "This pattern never matches because the pattern on line "+strconv.Itoa(p.tree.Line(first.Start)+1)+" catches everything it would.")
}

func globOf(w *syntax.Word) globPattern {
	var out globPattern
	for i, part := range w.Parts {
		switch part := part.(type) {
		case *syntax.Lit:
			value := part.Value
			if i == 0 && strings.HasPrefix(value, "~") {
				out = append(out, globToken{kind: globAny})
				_, rest, found := strings.Cut(value, "/")
				if !found {
					continue
				}
				value = "/" + rest
			}
			out = appendGlobLiteral(out, value, true)
		case *syntax.SglQuoted:
			out = appendGlobLiteral(out, part.Value, false)
		case *syntax.DblQuoted:
			for _, inner := range part.Parts {
				if lit, ok := inner.(*syntax.Lit); ok {
					out = appendGlobLiteral(out, lit.Value, false)
					continue
				}
				out = append(out, globToken{kind: globAny})
			}
		default:
			out = append(out, globToken{kind: globAny})
		}
	}
	return out
}

func appendGlobLiteral(out globPattern, s string, active bool) globPattern {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case !active:
		case c == '\\' && i+1 < len(s):
			i++
			c = s[i]
		case c == '*':
			out = append(out, globToken{kind: globAny})
			continue
		case c == '?':
			out = append(out, globToken{kind: globOne})
			continue
		case c == '[':
			if end := bracketEnd(s, i); end > 0 {
				out = append(out, globToken{kind: globOne})
				i = end
				continue
			}
		}
		out = append(out, globToken{kind: globChar, char: c})
	}
	return out
}

func exactGlob(w *syntax.Word) (globPattern, bool) {
	for i, part := range w.Parts {
		switch part := part.(type) {
		case *syntax.Lit:
			if (i == 0 && strings.HasPrefix(part.Value, "~")) || strings.ContainsAny(part.Value, "[]") {
				return nil, false
			}
		case *syntax.SglQuoted:
		case *syntax.DblQuoted:
			for _, inner := range part.Parts {
				if _, ok := inner.(*syntax.Lit); !ok {
					return nil, false
				}
			}
		default:
			return nil, false
		}
	}
	return globOf(w), true
}

func overlaps(a, b globPattern) bool {
	return solveGlobs(len(a), len(b), func(i, j int, below, right, diagonal bool) bool {
		switch {
		case i == len(a) && j == len(b):
			return true
		case i < len(a) && a[i].kind == globAny:
			return below || j < len(b) && right
		case j < len(b) && b[j].kind == globAny:
			return right || i < len(a) && below
		case i == len(a) || j == len(b):
			return false
		}
		return compatible(a[i], b[j]) && diagonal
	})
}

func compatible(a, b globToken) bool {
	return a.kind == globOne || b.kind == globOne || a.char == b.char
}

func covers(general, specific globPattern) bool {
	return solveGlobs(len(general), len(specific), func(i, j int, below, right, diagonal bool) bool {
		switch {
		case i == len(general):
			return j == len(specific)
		case general[i].kind == globAny:
			return below || j < len(specific) && right
		case j == len(specific), specific[j].kind == globAny:
			return false
		case specific[j].kind == globOne:
			return general[i].kind == globOne && diagonal
		}
		return (general[i].kind == globOne || general[i].char == specific[j].char) && diagonal
	})
}

func solveGlobs(rows, cols int, cell func(i, j int, below, right, diagonal bool) bool) bool {
	cells := make([]bool, 2*(cols+2))
	next, current := cells[:cols+2], cells[cols+2:]
	for i := rows; i >= 0; i-- {
		for j := cols; j >= 0; j-- {
			current[j] = cell(i, j, next[j], current[j+1], next[j+1])
		}
		next, current = current, next
	}
	return next[0]
}
