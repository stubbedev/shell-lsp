package lint

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

var parameterSyntaxFamily = family{
	codes: map[Code]meta{
		2082: {severity: SeverityError},
		2250: {severity: SeverityStyle, optional: "require-variable-braces"},
		2296: {severity: SeverityError},
		2297: {severity: SeverityError},
		2298: {severity: SeverityError},
		2299: {severity: SeverityError},
		2300: {severity: SeverityError},
		2301: {severity: SeverityError},
	},
	rules: []rule{
		{node: checkBracedExpansion, finish: checkUnparsedBracedExpansions},
		{node: checkUnbracedVariable},
	},
}

const specialParameterChars = "*@#?-$!"

func checkBracedExpansion(p *pass, n syntax.Node) {
	pe, ok := n.(*syntax.ParamExp)
	if !ok || pe.Short || !pe.Dollar.IsValid() || p.dialect == dialect.Zsh {
		return
	}
	p.checkBracedAt(parser.Offset(pe.Dollar))
}

func checkUnparsedBracedExpansions(p *pass) {
	if p.dialect == dialect.Zsh {
		return
	}
	lastLine := -1
	for _, e := range p.tree.Errors {
		line := p.tree.Line(e.Span.Start)
		if line == lastLine {
			continue
		}
		lastLine = line
		start := strings.LastIndexByte(p.text[:min(e.Span.Start, len(p.text))], '\n') + 1
		end := len(p.text)
		if i := strings.IndexByte(p.text[start:], '\n'); i >= 0 {
			end = start + i
		}
		p.checkBracedOnLine(start, end)
	}
}

func (p *pass) checkBracedOnLine(start, end int) {
	single, double := false, false
	for i := start; i < end; i++ {
		switch c := p.text[i]; {
		case c == '\\':
			i++
		case c == '\'' && !double:
			single = !single
		case single:
		case c == '"':
			double = !double
		case c == '#' && !double && (i == start || isBlankByte(p.text[i-1])):
			return
		case c == '$' && i+1 < end && p.text[i+1] == '{':
			i = p.checkBracedAt(i) - 1
		}
	}
}

func isBlankByte(c byte) bool { return c == ' ' || c == '\t' }

func (p *pass) checkBracedAt(dollar int) int {
	if dollar+2 < len(p.text) && strings.IndexByte(" \t\n|", p.text[dollar+2]) >= 0 {
		return dollar + 2
	}
	body, end, ok := scanBraced(p.text, dollar)
	if !ok {
		return dollar + 2
	}
	if isIndirection(p.text, body) {
		p.report(2082, parser.Span{Start: dollar, End: end}, "To expand a variable whose name is computed, use ${!name} or an associative array; "+p.text[dollar:end]+" nests expansions inside the name.")
		return end
	}
	if len(body) == 0 {
		return end
	}
	first := body[0]
	span := parser.Span{Start: first.start, End: first.end}
	text := p.text[first.start:first.end]
	switch first.kind {
	case braceLiteral:
		if c := text[0]; !isNameChar(c) && strings.IndexByte(specialParameterChars, c) < 0 {
			p.report(2296, span, "A parameter expansion cannot start with '"+text[:1]+"'; ${ must be followed by a variable name or special parameter.")
		}
	case braceDouble:
		if inner := strings.Trim(strings.TrimPrefix(text, "$"), `"`); isQuotableName(inner) {
			p.report(2297, span, "Double quotes are not allowed inside ${ }; write ${"+inner+"} and quote the whole expansion instead.")
			return end
		}
		p.report(2301, span, "This parameter expansion starts with unexpected quotes; ${ must be followed by a variable name.")
	case braceSingle:
		p.report(2301, span, "This parameter expansion starts with unexpected quotes; ${ must be followed by a variable name.")
	case braceVariable:
		p.report(2298, span, "${"+text+"...} is invalid: an expansion cannot be used as the name; drop the inner $ or use ${!name} for indirection.")
	case braceNested:
		if first.plain {
			p.report(2298, span, "${"+text+"...} is invalid: an expansion cannot be used as the name; drop the inner ${ } or use ${!name} for indirection.")
			return end
		}
		p.report(2299, span, "Parameter expansions cannot be nested; store the result of "+text+" in a variable first and expand that.")
	case braceCommand:
		p.report(2300, span, "Parameter expansion cannot operate on a command substitution; store the output of "+text+" in a variable first.")
	default:
		p.report(2301, span, "This parameter expansion starts with unexpected syntax; ${ must be followed by a variable name.")
	}
	return end
}

func isQuotableName(s string) bool {
	if len(s) == 1 {
		return isNameChar(s[0]) || strings.IndexByte(specialParameterChars, s[0]) >= 0
	}
	return parser.IsName(s)
}

func isIndirection(text string, body []bracePart) bool {
	expansions := 0
	for _, part := range body {
		switch part.kind {
		case braceLiteral:
			if !isNameChars(text[part.start:part.end]) {
				return false
			}
		case braceVariable, braceNested, braceCommand, braceArithmetic:
			expansions++
		default:
			return false
		}
	}
	return expansions > 0
}

func checkUnbracedVariable(p *pass, n syntax.Node) {
	pe, ok := n.(*syntax.ParamExp)
	if !ok || !pe.Short || !pe.Dollar.IsValid() || pe.Param == nil || !parser.IsName(pe.Param.Value) || p.namesAssignment(pe) {
		return
	}
	span, ok := p.span(pe)
	if !ok {
		return
	}
	name := pe.Param.Value
	fix := []textedit.Edit{{Start: span.Start, End: span.Start + 1, NewText: "${"}, {Start: span.End, End: span.End, NewText: "}"}}
	p.report(2250, span, "Write ${"+name+"} instead of $"+name+" to make the end of the variable name explicit.", fix...)
}

type bracePartKind uint8

const (
	braceLiteral bracePartKind = iota
	braceVariable
	braceNested
	braceCommand
	braceArithmetic
	braceSingle
	braceDouble
	braceExtGlob
)

type bracePart struct {
	kind       bracePartKind
	start, end int
	plain      bool
}

func scanBraced(text string, dollar int) ([]bracePart, int, bool) {
	var parts []bracePart
	literal := -1
	flush := func(at int) {
		if literal >= 0 {
			parts = append(parts, bracePart{kind: braceLiteral, start: literal, end: at})
			literal = -1
		}
	}
	for i := dollar + 2; i < len(text); {
		if text[i] == '}' {
			flush(i)
			return parts, i + 1, true
		}
		part, ok := scanBracePart(text, i)
		if !ok {
			return nil, 0, false
		}
		if part.kind == braceLiteral {
			if literal < 0 {
				literal = i
			}
			i = part.end
			continue
		}
		flush(i)
		parts = append(parts, part)
		i = part.end
	}
	return nil, 0, false
}

func scanBracePart(text string, i int) (bracePart, bool) {
	c := text[i]
	next := byte(0)
	if i+1 < len(text) {
		next = text[i+1]
	}
	switch {
	case c == '\\':
		return bracePart{kind: braceLiteral, start: i, end: min(i+2, len(text))}, true
	case c == '\'':
		return closeAt(braceSingle, text, i, i+1, "'")
	case c == '"':
		return scanDoubleQuoted(text, i, i+1)
	case c == '`':
		return closeAt(braceCommand, text, i, i+1, "`")
	case strings.IndexByte("@!*+?", c) >= 0 && next == '(':
		return closeParens(braceExtGlob, text, i, i+2, 1)
	case c != '$':
		return bracePart{kind: braceLiteral, start: i, end: i + 1}, true
	}
	switch {
	case next == '{':
		body, end, ok := scanBraced(text, i)
		if !ok {
			return bracePart{}, false
		}
		return bracePart{kind: braceNested, start: i, end: end, plain: isPlainBody(text, body)}, true
	case next == '(' && i+2 < len(text) && text[i+2] == '(':
		return closeParens(braceArithmetic, text, i, i+3, 2)
	case next == '(':
		return closeParens(braceCommand, text, i, i+2, 1)
	case next == '[':
		return closeAt(braceArithmetic, text, i, i+2, "]")
	case next == '\'':
		return closeAt(braceSingle, text, i, i+2, "'")
	case next == '"':
		return scanDoubleQuoted(text, i, i+2)
	case next != 0 && strings.IndexByte(specialParameterChars, next) >= 0, next >= '0' && next <= '9':
		return bracePart{kind: braceVariable, start: i, end: i + 2}, true
	case next != 0 && isNameChar(next):
		end := i + 1
		for end < len(text) && isNameChar(text[end]) {
			end++
		}
		return bracePart{kind: braceVariable, start: i, end: end}, true
	}
	return bracePart{kind: braceLiteral, start: i, end: i + 1}, true
}

func isPlainBody(text string, body []bracePart) bool {
	if len(body) != 1 || body[0].kind != braceLiteral {
		return false
	}
	inner := text[body[0].start:body[0].end]
	return parser.IsName(inner) || parser.IsDigits(inner) || (len(inner) == 1 && strings.IndexByte(specialParameterChars, inner[0]) >= 0)
}

func closeAt(kind bracePartKind, text string, start, from int, closer string) (bracePart, bool) {
	for i := from; i < len(text); i++ {
		switch {
		case text[i] == '\\' && kind != braceSingle:
			i++
		case strings.HasPrefix(text[i:], closer):
			return bracePart{kind: kind, start: start, end: i + len(closer)}, true
		}
	}
	return bracePart{}, false
}

func scanDoubleQuoted(text string, start, from int) (bracePart, bool) {
	for i := from; i < len(text); {
		switch text[i] {
		case '\\':
			i += 2
			continue
		case '"':
			return bracePart{kind: braceDouble, start: start, end: i + 1}, true
		case '$', '`':
			part, ok := scanBracePart(text, i)
			if !ok {
				return bracePart{}, false
			}
			i = part.end
			continue
		}
		i++
	}
	return bracePart{}, false
}

func closeParens(kind bracePartKind, text string, start, from, depth int) (bracePart, bool) {
	for i := from; i < len(text); {
		switch text[i] {
		case '\\':
			i += 2
			continue
		case '\'', '"', '`', '$':
			part, ok := scanBracePart(text, i)
			if !ok {
				return bracePart{}, false
			}
			i = part.end
			continue
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return bracePart{kind: kind, start: start, end: i + 1}, true
			}
		}
		i++
	}
	return bracePart{}, false
}
