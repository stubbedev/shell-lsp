package lint

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var assignmentsFamily = family{
	codes: map[Code]meta{
		2097: {severity: SeverityWarning},
		2098: {severity: SeverityWarning},
		2123: {severity: SeverityWarning},
		2141: {severity: SeverityWarning},
		2147: {severity: SeverityWarning},
		2190: {severity: SeverityWarning},
		2191: {severity: SeverityWarning},
		2192: {severity: SeverityWarning},
	},
	rules: []rule{
		{on: nodes(&syntax.ParamExp{}), node: checkPrefixAssignmentUse},
		{on: nodes(&syntax.Assign{}), node: checkSpecialAssignment},
		{on: nodes(&syntax.Assign{}), node: checkArrayLiteral},
	},
}

func checkPrefixAssignmentUse(p *pass, n syntax.Node) {
	pe, ok := n.(*syntax.ParamExp)
	if !ok || pe.Param == nil || pe.Names != 0 {
		return
	}
	call := p.enclosingCommand()
	if call == nil {
		return
	}
	span, ok := p.span(pe)
	if !ok {
		return
	}
	for _, a := range call.Assigns {
		if a.Name == nil || a.Name.Value != pe.Param.Value || a.Index != nil {
			continue
		}
		assigned, ok := p.span(a)
		if !ok || (assigned.Start <= span.Start && span.End <= assigned.End) {
			continue
		}
		name := a.Name.Value
		p.report(2097, assigned, "This "+name+"= only applies to the command's environment; the expansion of $"+name+" on the same line still sees the old value.")
		p.report(2098, span, "This expansion runs before the prefix assignment of "+name+" takes effect, so it sees the old value; assign it on its own line first.")
	}
}

func (p *pass) enclosingCommand() *syntax.CallExpr {
	for _, n := range slices.Backward(p.stack) {
		if call, ok := n.(*syntax.CallExpr); ok && len(call.Args) > 0 {
			return call
		}
	}
	return nil
}

func checkSpecialAssignment(p *pass, n syntax.Node) {
	a, ok := n.(*syntax.Assign)
	if !ok || a.Name == nil || a.Index != nil || a.Append || a.Naked {
		return
	}
	switch a.Name.Value {
	case "PATH":
		p.checkPathAssignment(a)
	case "IFS":
		p.checkFieldSeparator(a)
	}
}

func (p *pass) checkPathAssignment(a *syntax.Assign) {
	span, ok := p.span(a)
	if !ok {
		return
	}
	if quotedTilde(a.Value) {
		p.report(2147, span, "A quoted ~ is not expanded in PATH; use $HOME instead.")
	}
	call, ok := p.parent().(*syntax.CallExpr)
	if !ok || len(call.Args) > 0 {
		return
	}
	text, literal := pathText(a.Value)
	if strings.Contains(text, "/bin") || strings.Contains(text, "/sbin") || strings.Contains(text, ":") {
		return
	}
	if strings.Contains(text, "/") || literal {
		p.report(2123, span, "PATH is the command search path; this replaces it with something that is not a list of bin directories. Use another variable name, or append with PATH=$PATH:dir.")
	}
}

func quotedTilde(w *syntax.Word) bool {
	if w == nil {
		return false
	}
	for _, part := range w.Parts {
		switch part := part.(type) {
		case *syntax.SglQuoted:
			if strings.Contains(part.Value, "~") {
				return true
			}
		case *syntax.DblQuoted:
			for _, inner := range part.Parts {
				if lit, ok := inner.(*syntax.Lit); ok && strings.Contains(lit.Value, "~") {
					return true
				}
			}
		}
	}
	return false
}

func pathText(w *syntax.Word) (string, bool) {
	if w == nil {
		return "", true
	}
	var sb strings.Builder
	literal := true
	var add func(parts []syntax.WordPart)
	add = func(parts []syntax.WordPart) {
		for _, part := range parts {
			switch part := part.(type) {
			case *syntax.Lit:
				sb.WriteString(part.Value)
			case *syntax.SglQuoted:
				sb.WriteString(part.Value)
			case *syntax.DblQuoted:
				add(part.Parts)
			default:
				literal = false
				sb.WriteByte('x')
			}
		}
	}
	add(w.Parts)
	return sb.String(), literal
}

func (p *pass) checkFieldSeparator(a *syntax.Assign) {
	if a.Value == nil {
		return
	}
	value, ok := decodedLiteral(a.Value)
	if !ok {
		return
	}
	span, ok := p.span(a.Value)
	if !ok {
		return
	}
	newline, tab := "a literal newline in quotes, or IFS=$(printf '\\n.') followed by IFS=${IFS%.}", "a literal tab in quotes"
	if p.dialect == dialect.Bash || p.dialect == dialect.Ksh {
		newline, tab = "IFS=$'\\n'", "IFS=$'\\t'"
	}
	switch {
	case value == `\n`:
		p.report(2141, span, `IFS="\n" splits on backslash and the letter n, not on newlines; use `+newline+".")
	case value == `\t`:
		p.report(2141, span, `IFS="\t" splits on backslash and the letter t, not on tabs; use `+tab+".")
	case strings.Contains(value, `\`):
		p.report(2141, span, "This IFS contains a literal backslash; escapes like \\n are not interpreted inside quotes.")
	case strings.Contains(value, "n"):
		p.report(2141, span, "This IFS contains the letter n; did you mean a newline?")
	case strings.Contains(value, "t"):
		p.report(2141, span, "This IFS contains the letter t; did you mean a tab?")
	}
}

func decodedLiteral(w *syntax.Word) (string, bool) {
	var sb strings.Builder
	for _, part := range w.Parts {
		sq, ok := part.(*syntax.SglQuoted)
		if !ok || !sq.Dollar {
			lit, ok := parser.Literal(&syntax.Word{Parts: []syntax.WordPart{part}})
			if !ok {
				return "", false
			}
			sb.WriteString(lit)
			continue
		}
		sb.WriteString(decodeANSIC(sq.Value))
	}
	return sb.String(), true
}

var ansiCEscapes = map[byte]byte{
	'a': '\a', 'b': '\b', 'e': 0x1b, 'E': 0x1b, 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t', 'v': '\v',
	'\\': '\\', '\'': '\'', '"': '"', '?': '?',
}

func decodeANSIC(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			sb.WriteByte(s[i])
			continue
		}
		i++
		if c, ok := ansiCEscapes[s[i]]; ok {
			sb.WriteByte(c)
			continue
		}
		if s[i] == 'x' {
			if c, n := escapedNumber(s[i+1:], 16, 2); n > 0 {
				sb.WriteByte(c)
				i += n
				continue
			}
		}
		if c, n := escapedNumber(s[i:], 8, 3); n > 0 {
			sb.WriteByte(c)
			i += n - 1
			continue
		}
		sb.WriteByte('\\')
		sb.WriteByte(s[i])
	}
	return sb.String()
}

func escapedNumber(s string, base, width int) (byte, int) {
	var code uint
	end := 0
	for ; end < min(width, len(s)); end++ {
		digit, ok := digitValue(s[end])
		if !ok || digit >= uint(base) {
			break
		}
		code = code*uint(base) + digit
	}
	return byte(code), end
}

func digitValue(c byte) (uint, bool) {
	switch {
	case c >= '0' && c <= '9':
		return uint(c - '0'), true
	case c >= 'a' && c <= 'f':
		return uint(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return uint(c-'A') + 10, true
	}
	return 0, false
}

func checkArrayLiteral(p *pass, n syntax.Node) {
	a, ok := n.(*syntax.Assign)
	if !ok || a.Array == nil || a.Index != nil || a.Name == nil {
		return
	}
	associative := p.associativeArray(a.Name.Value) && !p.dialect.Features().AssociativePairs
	for _, elem := range a.Array.Elems {
		switch {
		case elem.Index != nil:
			p.checkIndexedElement(elem)
		case elem.Value == nil:
		case p.numberedElement(elem.Value):
		case associative:
			if span, ok := p.span(elem.Value); ok {
				p.report(2190, span, "Elements of an associative array need a key: write [key]=value.")
			}
		}
	}
}

func (p *pass) checkIndexedElement(elem *syntax.ArrayElem) {
	closing := parser.Offset(elem.Index.End())
	if closing >= len(p.text) || p.text[closing] != ']' {
		return
	}
	equals := closing + 1 + len(p.text[closing+1:]) - len(strings.TrimLeft(p.text[closing+1:], " \t"))
	if equals >= len(p.text) || p.text[equals] != '=' {
		return
	}
	at := parser.Span{Start: equals, End: equals + 1}
	if equals > closing+1 {
		edits := []textedit.Edit{{Start: closing + 1, End: equals}}
		value := equals + 1 + len(p.text[equals+1:]) - len(strings.TrimLeft(p.text[equals+1:], " \t"))
		if value > equals+1 && value < len(p.text) && !strings.ContainsRune(")\n#", rune(p.text[value])) {
			edits = append(edits, textedit.Edit{Start: equals + 1, End: value})
		}
		p.report(2191, at, "This = stands alone, so it is literal text rather than part of [index]=value; remove the spaces around it.", reviewFix("Remove the spaces around =", edits...))
		return
	}
	if start, ok := elementValueStart(elem); !ok || start != equals+1 {
		empty := parser.Span{Start: equals + 1, End: equals + 1}
		p.report(2192, empty, "This array element has an index but no value; remove the space after = or quote an empty value.")
	}
}

func elementValueStart(elem *syntax.ArrayElem) (int, bool) {
	switch {
	case elem.Value != nil:
		return parser.Offset(elem.Value.Pos()), true
	case elem.Array != nil:
		return parser.Offset(elem.Array.Pos()), true
	}
	return 0, false
}

func (p *pass) numberedElement(w *syntax.Word) bool {
	found := false
	for _, part := range w.Parts {
		lit, ok := part.(*syntax.Lit)
		if !ok {
			continue
		}
		prefix, _, hasEquals := strings.Cut(lit.Value, "=")
		if !hasEquals || strings.Trim(prefix, "0123456789") != "" {
			continue
		}
		found = true
		span, ok := p.span(lit)
		if !ok {
			continue
		}
		p.report(2191, span, "This = is literal text in an array element; to set an index write [index]=value, otherwise quote it.", p.quoteFix(span, Safe))
	}
	return found
}
