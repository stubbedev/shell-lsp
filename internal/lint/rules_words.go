package lint

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var wordsFamily = family{
	codes: map[Code]meta{
		2025: {severity: SeverityInfo},
		2026: {severity: SeverityInfo},
		2027: {severity: SeverityWarning},
		2054: {severity: SeverityWarning},
		2088: {severity: SeverityWarning},
		2140: {severity: SeverityWarning},
		2254: {severity: SeverityWarning},
	},
	rules: []rule{
		{on: nodes(&syntax.Word{}), node: checkWordQuoting},
		{on: nodes(&syntax.Assign{}), node: checkPromptEscapes},
		{on: nodes(&syntax.ArrayExpr{}), node: checkArrayCommas},
		{on: nodes(&syntax.CaseItem{}), node: checkCasePatterns},
	},
}

func checkWordQuoting(p *pass, n syntax.Node) {
	word, ok := n.(*syntax.Word)
	if !ok {
		return
	}
	checkQuotedTilde(p, word)
	parts := word.Parts
	for i, part := range parts {
		if i > 0 {
			checkUnquotedBetweenSingle(p, parts[i-1], part)
		}
		if i > 0 && i+1 < len(parts) && isDoubleQuoted(parts[i-1]) && isDoubleQuoted(parts[i+1]) && !insideBraceExpansion(parts[:i]) {
			checkUnquotedBetweenDouble(p, word, parts[i-1], part, parts[i+1])
		}
	}
}

func insideBraceExpansion(before []syntax.WordPart) bool {
	depth := 0
	for _, part := range before {
		if lit, ok := part.(*syntax.Lit); ok {
			depth = braceDepth(depth, lit.Value, nil)
		}
	}
	return depth > 0
}

func isDoubleQuoted(part syntax.WordPart) bool {
	_, ok := part.(*syntax.DblQuoted)
	return ok
}

func checkUnquotedBetweenSingle(p *pass, prev, part syntax.WordPart) {
	if _, ok := prev.(*syntax.SglQuoted); !ok {
		return
	}
	lit, ok := part.(*syntax.Lit)
	if !ok || lit.Value == "" || strings.Trim(lit.Value, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") != "" {
		return
	}
	if span, ok := p.span(lit); ok {
		p.report(2026, span, lit.Value+" sits between single-quoted strings, so it is not quoted; move it inside the quotes if that was the intent.")
	}
}

func checkUnquotedBetweenDouble(p *pass, word *syntax.Word, before, part, after syntax.WordPart) {
	switch part := part.(type) {
	case *syntax.ParamExp:
		p.reportTrapped(part)
	case *syntax.CmdSubst:
		if !part.Backquotes && !part.TempFile && !part.ReplyVar {
			p.reportTrapped(part)
		}
	case *syntax.Lit:
		if part.Value == "=" || part.Value == ":" || part.Value == "/" || isExpandingPart(part) ||
			(isSoleExpansion(before) && isSoleExpansion(after)) || p.insideExpansionOrRegex(word) {
			return
		}
		if span, ok := p.span(part); ok {
			p.report(2140, span, part.Value+" ends one double-quoted string and starts another; escape the inner quotes as \\\" if they are meant literally.")
		}
	}
}

func (p *pass) reportTrapped(part syntax.WordPart) {
	if span, ok := p.span(part); ok {
		p.report(2027, span, "The quotes around "+p.text[span.Start:span.End]+" close and reopen the string, leaving it unquoted; drop the inner quotes or escape them.")
	}
}

func isSoleExpansion(part syntax.WordPart) bool {
	dq, ok := part.(*syntax.DblQuoted)
	if !ok || len(dq.Parts) != 1 {
		return false
	}
	switch inner := dq.Parts[0].(type) {
	case *syntax.ParamExp:
		return true
	case *syntax.CmdSubst:
		return !inner.TempFile && !inner.ReplyVar
	}
	return false
}

func (p *pass) insideExpansionOrRegex(word *syntax.Word) bool {
	if bin, ok := p.parent().(*syntax.BinaryTest); ok && bin.Op == syntax.TsReMatch && bin.Y == word {
		return true
	}
	for depth := 0; ; depth++ {
		switch p.ancestor(depth).(type) {
		case nil, *syntax.Stmt:
			return false
		case *syntax.ParamExp:
			return true
		}
	}
}

func checkQuotedTilde(p *pass, word *syntax.Word) {
	if len(word.Parts) == 0 {
		return
	}
	var at syntax.Node
	switch first := word.Parts[0].(type) {
	case *syntax.SglQuoted:
		if !first.Dollar && strings.HasPrefix(first.Value, "~/") {
			at = first
		}
	case *syntax.DblQuoted:
		if len(first.Parts) == 0 {
			return
		}
		if lit, ok := first.Parts[0].(*syntax.Lit); ok && strings.HasPrefix(lit.Value, "~/") {
			at = lit
		}
	}
	if at == nil {
		return
	}
	if span, ok := p.span(at); ok {
		p.report(2088, span, "A quoted ~ is not expanded to the home directory; use \"$HOME\" or leave the ~/ unquoted.")
	}
}

func checkPromptEscapes(p *pass, n syntax.Node) {
	assign, ok := n.(*syntax.Assign)
	if !ok || assign.Name == nil || assign.Name.Value != "PS1" || assign.Value == nil {
		return
	}
	var flat strings.Builder
	flattenLiteral(&flat, assign.Value.Parts)
	text := flat.String()
	if open := strings.Index(text, `\[`); open >= 0 {
		if end := strings.LastIndex(text, `\]`); end > open {
			text = text[:open] + text[end+2:]
		}
	}
	if !strings.Contains(text, `\x1B`) && !strings.Contains(text, `\x1b`) && !strings.Contains(text, `\e`) &&
		!strings.Contains(text, "\x1b") && !strings.Contains(text, `\033`) {
		return
	}
	if span, ok := p.span(assign.Value); ok {
		p.report(2025, span, `Wrap terminal escape sequences in PS1 with \[ and \] so bash knows they take no space; otherwise line editing misplaces the cursor.`)
	}
}

func flattenLiteral(sb *strings.Builder, parts []syntax.WordPart) {
	for _, part := range parts {
		switch part := part.(type) {
		case *syntax.Lit:
			sb.WriteString(part.Value)
		case *syntax.SglQuoted:
			sb.WriteString(part.Value)
		case *syntax.DblQuoted:
			flattenLiteral(sb, part.Parts)
		}
	}
}

func checkArrayCommas(p *pass, n syntax.Node) {
	array, ok := n.(*syntax.ArrayExpr)
	if !ok {
		return
	}
	for _, elem := range array.Elems {
		if elem.Value == nil || !hasUnbracedComma(elem.Value) {
			continue
		}
		if span, ok := p.span(array); ok {
			p.report(2054, span, "Array elements are separated by whitespace, not commas; the commas become part of the values.")
		}
		return
	}
}

func hasUnbracedComma(w *syntax.Word) bool {
	found := false
	for _, part := range w.Parts[:parser.GlobQualifiers(w, true)] {
		if lit, ok := part.(*syntax.Lit); ok {
			braceDepth(0, lit.Value, func() { found = true })
		}
	}
	return found
}

func braceDepth(depth int, lit string, topLevelComma func()) int {
	for i := 0; i < len(lit); i++ {
		switch lit[i] {
		case '\\':
			i++
		case '{':
			depth++
		case '}':
			depth = max(depth-1, 0)
		case ',':
			if depth == 0 && topLevelComma != nil {
				topLevelComma()
			}
		}
	}
	return depth
}

func checkCasePatterns(p *pass, n syntax.Node) {
	item, ok := n.(*syntax.CaseItem)
	if !ok {
		return
	}
	for _, pattern := range item.Patterns {
		if isGlob(pattern) || !hasUnquotedExpansion(pattern) {
			continue
		}
		if span, ok := p.span(pattern); ok {
			p.report(2254, span, "The unquoted expansion in case pattern "+p.text[span.Start:span.End]+" is matched as a glob; quote it to match its value literally.", p.soleExpansionQuoteFix(pattern, span)...)
		}
	}
}

func hasUnquotedExpansion(w *syntax.Word) bool {
	for _, part := range w.Parts {
		switch part.(type) {
		case *syntax.ParamExp, *syntax.CmdSubst:
			return true
		}
	}
	return false
}
