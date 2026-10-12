package lint

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/internal/textline"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const codeBadSubstitution = nativeBase + 2008

var parameterSyntaxFamily = family{
	codes: map[Code]meta{
		codeBadSubstitution: {severity: SeverityError},
		2082:                {severity: SeverityError, summary: "An expansion nested inside a parameter name does not compute the variable to expand."},
		2250:                {severity: SeverityStyle, optional: optionalVariableBraces, summary: "A variable is expanded without braces."},
		2296:                {severity: SeverityError, summary: "A parameter expansion starts with a character no parameter name can start with."},
		2297:                {severity: SeverityError, summary: "Double quotes are used inside ${ }."},
		2298:                {severity: SeverityError, summary: "An expansion is used as the name in ${ }."},
		2299:                {severity: SeverityError, summary: "Parameter expansions cannot be nested."},
		2300:                {severity: SeverityError, summary: "Parameter expansion cannot operate on a command substitution."},
		2301:                {severity: SeverityError, summary: "A parameter expansion starts with quotes or other unexpected syntax."},
	},
	rules: []rule{
		{on: nodes(&syntax.ParamExp{}, &syntax.Lit{}), node: checkBracedExpansion, finish: checkUnparsedBracedExpansions},
		{on: nodes(&syntax.ParamExp{}), node: checkUnbracedVariable},
	},
}

const specialParameterChars = "*@#?-$!"

func checkBracedExpansion(p *pass, n syntax.Node) {
	if p.dialect == dialect.Zsh {
		return
	}
	switch n := n.(type) {
	case *syntax.ParamExp:
		if !n.Short && n.Dollar.IsValid() {
			p.checkBracedAt(parser.Offset(n.Dollar))
		}
	case *syntax.Lit:
		if !p.tree.Unexpandable(n) {
			return
		}
		if _, reported := p.reportBraced(parser.Offset(n.ValuePos)); !reported {
			p.reportNode(codeBadSubstitution, n, n.Value+" is not a valid parameter expansion, so the shell stops with \"bad substitution\" when it reaches this line.")
		}
	}
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
		start, end := textline.Bounds(p.text, min(e.Span.Start, len(p.text)))
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

func (p *pass) startsParameter(c byte) bool {
	return parser.IsNameChar(c) || strings.IndexByte(specialParameterChars, c) >= 0 || c == '.' && p.tree.Lang == syntax.LangKsh93
}

func (p *pass) braces() *braceScanner {
	if p.braceScan == nil {
		p.braceScan = &braceScanner{text: p.text}
	}
	return p.braceScan
}

func (p *pass) checkBracedAt(dollar int) int {
	end, _ := p.reportBraced(dollar)
	return end
}

func (p *pass) reportBraced(dollar int) (int, bool) {
	if dollar+2 < len(p.text) && strings.IndexByte(" \t\n|", p.text[dollar+2]) >= 0 {
		return dollar + 2, false
	}
	body, end, ok := p.braces().braced(dollar)
	if !ok {
		return dollar + 2, false
	}
	if isIndirection(p.text, body) {
		p.report(2082, parser.Span{Start: dollar, End: end}, "To expand a variable whose name is computed, use ${!name} or an associative array; "+p.text[dollar:end]+" nests expansions inside the name.")
		return end, true
	}
	if len(body) == 0 {
		return end, false
	}
	first := body[0]
	span := parser.Span{Start: first.start, End: first.end}
	text := p.text[first.start:first.end]
	switch first.kind {
	case braceLiteral:
		if p.startsParameter(text[0]) {
			return end, false
		}
		p.report(2296, span, "A parameter expansion cannot start with '"+text[:1]+"'; ${ must be followed by a variable name or special parameter.")
	case braceDouble:
		if inner := strings.Trim(strings.TrimPrefix(text, "$"), `"`); isQuotableName(inner) {
			p.report(2297, span, "Double quotes are not allowed inside ${ }; write ${"+inner+"} and quote the whole expansion instead.")
			return end, true
		}
		p.report(2301, span, "This parameter expansion starts with unexpected quotes; ${ must be followed by a variable name.")
	case braceSingle:
		p.report(2301, span, "This parameter expansion starts with unexpected quotes; ${ must be followed by a variable name.")
	case braceVariable:
		p.report(2298, span, "${"+text+"...} is invalid: an expansion cannot be used as the name; drop the inner $ or use ${!name} for indirection.")
	case braceNested:
		if first.plain {
			p.report(2298, span, "${"+text+"...} is invalid: an expansion cannot be used as the name; drop the inner ${ } or use ${!name} for indirection.")
			return end, true
		}
		p.report(2299, span, "Parameter expansions cannot be nested; store the result of "+text+" in a variable first and expand that.")
	case braceCommand:
		p.report(2300, span, "Parameter expansion cannot operate on a command substitution; store the output of "+text+" in a variable first.")
	default:
		p.report(2301, span, "This parameter expansion starts with unexpected syntax; ${ must be followed by a variable name.")
	}
	return end, true
}

func isQuotableName(s string) bool {
	if len(s) == 1 {
		return parser.IsNameChar(s[0]) || strings.IndexByte(specialParameterChars, s[0]) >= 0
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
	if !ok || !p.active(2250) || !pe.Short || !pe.Dollar.IsValid() || pe.Param == nil || !parser.IsName(pe.Param.Value) || p.namesAssignment(pe) {
		return
	}
	span, ok := p.span(pe)
	if !ok {
		return
	}
	text := p.text[span.Start:parser.Offset(pe.Param.Pos())] + pe.Param.Value
	if pe.Index != nil {
		text += p.text[parser.Offset(pe.Param.Pos())+len(pe.Param.Value) : parser.Offset(pe.Index.End())+len("]")]
	}
	message := "Write ${" + text[1:] + "} instead of " + text + " to make the end of the variable name explicit."
	if !strings.HasPrefix(p.text[span.Start:], text) {
		p.report(2250, span, message)
		return
	}
	end := span.Start + len(text)
	p.report(2250, span, message, safeFix("Write ${"+text[1:]+"}", textedit.Edit{Start: span.Start, End: span.Start + 1, NewText: "${"}, textedit.Edit{Start: end, End: end, NewText: "}"}))
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

type braceScanner struct {
	text    string
	depth   int
	scanned map[int]scannedPart
}

type scannedPart struct {
	part bracePart
	ok   bool
}

const maxBraceScanDepth = 1000

func (s *braceScanner) braced(dollar int) ([]bracePart, int, bool) {
	var parts []bracePart
	literal := -1
	flush := func(at int) {
		if literal >= 0 {
			parts = append(parts, bracePart{kind: braceLiteral, start: literal, end: at})
			literal = -1
		}
	}
	for i := dollar + 2; i < len(s.text); {
		if s.text[i] == '}' {
			flush(i)
			return parts, i + 1, true
		}
		part, ok := s.part(i)
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

func (s *braceScanner) part(i int) (bracePart, bool) {
	if done, ok := s.scanned[i]; ok {
		return done.part, done.ok
	}
	if s.depth >= maxBraceScanDepth {
		return bracePart{}, false
	}
	s.depth++
	part, ok := s.scanPart(i)
	s.depth--
	if !ok || part.kind != braceLiteral {
		if s.scanned == nil {
			s.scanned = map[int]scannedPart{}
		}
		s.scanned[i] = scannedPart{part: part, ok: ok}
	}
	return part, ok
}

func (s *braceScanner) scanPart(i int) (bracePart, bool) {
	text := s.text
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
		return s.doubleQuoted(i, i+1)
	case c == '`':
		return closeAt(braceCommand, text, i, i+1, "`")
	case strings.IndexByte("@!*+?", c) >= 0 && next == '(':
		return s.closeParens(braceExtGlob, i, i+2, 1)
	case c != '$':
		return bracePart{kind: braceLiteral, start: i, end: i + 1}, true
	}
	switch {
	case next == '{':
		body, end, ok := s.braced(i)
		if !ok {
			return bracePart{}, false
		}
		return bracePart{kind: braceNested, start: i, end: end, plain: isPlainBody(text, body)}, true
	case next == '(' && i+2 < len(text) && text[i+2] == '(':
		return s.closeParens(braceArithmetic, i, i+3, 2)
	case next == '(':
		return s.closeParens(braceCommand, i, i+2, 1)
	case next == '[':
		return closeAt(braceArithmetic, text, i, i+2, "]")
	case next == '\'':
		return closeAt(braceSingle, text, i, i+2, "'")
	case next == '"':
		return s.doubleQuoted(i, i+2)
	case next != 0 && strings.IndexByte(specialParameterChars, next) >= 0, next >= '0' && next <= '9':
		return bracePart{kind: braceVariable, start: i, end: i + 2}, true
	case next != 0 && parser.IsNameChar(next):
		end := i + 1
		for end < len(text) && parser.IsNameChar(text[end]) {
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

func (s *braceScanner) doubleQuoted(start, from int) (bracePart, bool) {
	for i := from; i < len(s.text); {
		switch s.text[i] {
		case '\\':
			i += 2
			continue
		case '"':
			return bracePart{kind: braceDouble, start: start, end: i + 1}, true
		case '$', '`':
			part, ok := s.part(i)
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

func (s *braceScanner) closeParens(kind bracePartKind, start, from, depth int) (bracePart, bool) {
	for i := from; i < len(s.text); {
		switch s.text[i] {
		case '\\':
			i += 2
			continue
		case '\'', '"', '`', '$':
			part, ok := s.part(i)
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
