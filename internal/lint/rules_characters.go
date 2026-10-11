package lint

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var charactersFamily = family{
	codes: map[Code]meta{
		1017: {severity: SeverityError},
		1018: {severity: SeverityError},
		1077: {severity: SeverityError},
		1078: {severity: SeverityWarning},
		1079: {severity: SeverityInfo},
		1083: {severity: SeverityWarning},
		1087: {severity: SeverityError},
		1101: {severity: SeverityError},
		1109: {severity: SeverityError},
		1110: {severity: SeverityWarning},
		1111: {severity: SeverityWarning},
		1112: {severity: SeverityWarning},
		1132: {severity: SeverityWarning},
		1135: {severity: SeverityStyle},
		1143: {severity: SeverityError},
	},
	rules: []rule{
		{on: nodes(&syntax.Lit{}), node: checkUnicodeSpaces},
		{finish: checkCarriageReturns},
		{on: nodes(&syntax.Lit{}, &syntax.SglQuoted{}), node: checkLiteralCharacters},
		{on: nodes(&syntax.DblQuoted{}, &syntax.SglQuoted{}, &syntax.CmdSubst{}), node: checkSuspiciousClosingQuote},
		{on: nodes(&syntax.Word{}), node: checkLiteralBraces},
		{on: nodes(&syntax.ParamExp{}), node: checkUnbracedSubscript},
		{on: nodes(&syntax.Stmt{}), node: checkBackgroundAmpersand},
		{on: nodes(&syntax.DblQuoted{}), node: checkQuotedDollar},
		{on: nodes(&syntax.CallExpr{}), node: checkCommentedContinuation},
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
	offsets := p.carriageReturnOffsets()
	if len(offsets) == 0 {
		return
	}
	verbatim := p.verbatimRegions()
	next := 0
	for _, i := range offsets {
		if i > 0 && p.text[i-1] == '\\' {
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

func (p *pass) carriageReturnOffsets() []int {
	offsets := slices.Clone(p.carriageReturns)
	for i := range len(p.text) {
		if p.text[i] == '\r' {
			offsets = append(offsets, i)
		}
	}
	slices.Sort(offsets)
	return slices.Compact(offsets)
}

func (p *pass) verbatimRegions() []parser.Span {
	var out []parser.Span
	parser.Walk(p.tree.File, func(n syntax.Node) bool {
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

func lineStart(text string, offset int) int {
	return strings.LastIndexByte(text[:offset], '\n') + 1
}

func onlyBlanksBefore(text string, offset int) bool {
	return strings.TrimLeft(text[lineStart(text, offset):offset], " \t") == ""
}

func lineEnd(text string, from int) int {
	from = min(from, len(text))
	if i := strings.IndexByte(text[from:], '\n'); i >= 0 {
		return from + i
	}
	return len(text)
}

const (
	batsTest          = "@test"
	acuteAccent       = '\u00b4'
	singleQuoteLike   = "\u2018\u2019"
	doubleQuoteLike   = "\u201c\u201d\u2033\u2036"
	htmlEntityMessage = "This looks like an HTML entity copied from a web page; write the character itself (& > or <) instead."
)

func checkLiteralCharacters(p *pass, n syntax.Node) {
	switch n := n.(type) {
	case *syntax.Lit:
		p.checkLitCharacters(n)
	case *syntax.SglQuoted:
		if span, ok := p.span(n); ok && !n.Dollar {
			p.reportCharacters(1112, span.Start+1, n.Value, singleQuoteLike, "This curly quote is a literal character inside single quotes; if it was meant to end the string, type a plain '.")
		}
	}
}

func (p *pass) checkLitCharacters(lit *syntax.Lit) {
	span, ok := p.span(lit)
	if !ok {
		return
	}
	switch parent := p.parent().(type) {
	case *syntax.DblQuoted:
		p.reportCharacters(1111, span.Start, lit.Value, doubleQuoteLike, "This curly quote is a literal character inside double quotes; if it was meant to end the string, type a plain \".")
	case *syntax.Word:
		if r, ok := p.heredocOf(parent); ok {
			if !p.quotedHeredoc(r) {
				p.reportCharacters(1111, span.Start, lit.Value, doubleQuoteLike, "This curly quote is a literal character in the here-document; type a plain \" if a real quote was intended.")
			}
			return
		}
		p.reportCharacters(1110, span.Start, lit.Value, singleQuoteLike+doubleQuoteLike, "This curly quote is a literal character, not shell quoting; type a plain ' or \" instead.")
		p.reportCharacters(1077, span.Start, lit.Value, string(acuteAccent), "This is an acute accent (´), not a backtick; use $(...) for command substitution.")
		p.checkEscapedTrailingSpace(span)
	}
}

func (p *pass) heredocOf(w *syntax.Word) (*syntax.Redirect, bool) {
	r, ok := p.ancestor(1).(*syntax.Redirect)
	return r, ok && r.Hdoc == w
}

func (p *pass) quotedHeredoc(r *syntax.Redirect) bool {
	if p.quotedHeredocs == nil {
		p.quotedHeredocs = make(map[int]bool, len(p.tree.Heredocs))
		for _, h := range p.tree.Heredocs {
			p.quotedHeredocs[h.Span.Start] = h.Quoted
		}
	}
	return p.quotedHeredocs[parser.Offset(r.OpPos)]
}

func (p *pass) reportCharacters(code Code, start int, value, set, message string) {
	if isASCII(value) {
		return
	}
	for i := 0; i < len(value); {
		r, size := utf8.DecodeRuneInString(value[i:])
		if strings.ContainsRune(set, r) {
			p.report(code, parser.Span{Start: start + i, End: start + i + size}, message)
		}
		i += size
	}
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

func (p *pass) checkEscapedTrailingSpace(span parser.Span) {
	end := span.End
	if end < 2 || p.text[end-1] != ' ' || p.text[end-2] != '\\' || escaped(p.text, end-2) {
		return
	}
	rest := strings.TrimLeft(p.text[end:], " \t")
	if rest == "" || rest[0] == '\n' {
		p.report(1101, parser.Span{Start: end - 2, End: end}, "This backslash escapes a space instead of the line break, so the command ends here; delete the whitespace after the backslash.")
	}
}

func checkSuspiciousClosingQuote(p *pass, n syntax.Node) {
	var open int
	var continues string
	switch n := n.(type) {
	case *syntax.DblQuoted:
		open, continues = 1, `$"`
		if n.Dollar {
			open = 2
		}
	case *syntax.SglQuoted:
		open, continues = 1, "'"
		if n.Dollar {
			open = 2
		}
	case *syntax.CmdSubst:
		if !n.Backquotes {
			return
		}
		open = 1
	default:
		return
	}
	span, ok := p.span(n)
	if !ok || span.End >= len(p.text) || span.End-span.Start < open+1 {
		return
	}
	if newline := strings.IndexByte(p.text[span.Start+open:span.End-1], '\n'); newline <= 0 {
		return
	}
	next := p.text[span.End]
	if !parser.IsNameChar(next) && next != '%' && strings.IndexByte(continues, next) < 0 {
		return
	}
	if _, single := n.(*syntax.SglQuoted); single && isASCIILetter(p.text[span.End-2]) && isASCIILetter(next) {
		return
	}
	p.report(1078, parser.Span{Start: span.Start, End: span.Start + open}, "This quote spans several lines and is closed right before more text; check that it is closed where you meant.")
	p.report(1079, parser.Span{Start: span.End - 1, End: span.End}, "This looks like it was meant to open a new quote, but it closes the multi-line one that started earlier.")
}

func checkLiteralBraces(p *pass, n syntax.Node) {
	w, ok := n.(*syntax.Word)
	if !ok || p.dialect == dialect.Zsh {
		return
	}
	switch p.parent().(type) {
	case *syntax.CallExpr, *syntax.Assign, *syntax.WordIter, *syntax.ArrayElem:
	default:
		return
	}
	if !p.hasLiteralBrace(w) || p.onBatsTestLine(w) {
		return
	}
	shape, offsets := braceShape(p, w)
	var open []int
	for i := range shape {
		switch shape[i] {
		case '{':
			open = append(open, i)
		case '}':
			if len(open) == 0 {
				p.reportLiteralBrace(offsets[i])
				continue
			}
			start := open[len(open)-1]
			open = open[:len(open)-1]
			if !isBraceExpansion(shape[start+1 : i]) {
				p.reportLiteralBrace(offsets[start])
				p.reportLiteralBrace(offsets[i])
			}
		}
	}
	for _, i := range open {
		p.reportLiteralBrace(offsets[i])
	}
}

func (p *pass) hasLiteralBrace(w *syntax.Word) bool {
	for _, part := range w.Parts {
		if lit, ok := p.literalPart(part); ok && strings.ContainsAny(lit.Value, "{}") {
			return true
		}
	}
	return false
}

func (p *pass) literalPart(part syntax.WordPart) (*syntax.Lit, bool) {
	lit, ok := part.(*syntax.Lit)
	return lit, ok && !p.tree.Unexpandable(lit)
}

func (p *pass) onBatsTestLine(w *syntax.Word) bool {
	start := parser.Offset(w.Pos())
	return strings.HasPrefix(strings.TrimLeft(p.text[lineStart(p.text, start):start], " \t"), batsTest+" ")
}

func (p *pass) reportLiteralBrace(at int) {
	p.report(1083, parser.Span{Start: at, End: at + 1}, "This "+p.text[at:at+1]+" is a literal character, not part of a brace expansion or block; quote or escape it if that is intended.")
}

func braceShape(p *pass, w *syntax.Word) ([]byte, []int) {
	var shape []byte
	var offsets []int
	for _, part := range w.Parts {
		span, ok := p.span(part)
		if !ok {
			continue
		}
		lit, isLit := p.literalPart(part)
		if !isLit {
			shape, offsets = append(shape, 'x'), append(offsets, span.Start)
			continue
		}
		for i := 0; i < len(lit.Value); i++ {
			c := lit.Value[i]
			if c == '\\' && i+1 < len(lit.Value) {
				c = 'x'
				shape, offsets = append(shape, c), append(offsets, span.Start+i)
				i++
				continue
			}
			shape, offsets = append(shape, c), append(offsets, span.Start+i)
		}
	}
	return shape, offsets
}

func isBraceExpansion(inner []byte) bool {
	if len(inner) == 0 {
		return true
	}
	depth := 0
	for _, c := range inner {
		switch c {
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				return true
			}
		}
	}
	from, to, ok := strings.Cut(string(inner), "..")
	return ok && from != "" && to != ""
}

func checkUnbracedSubscript(p *pass, n syntax.Node) {
	pe, ok := n.(*syntax.ParamExp)
	if !ok || !pe.Short || pe.Param == nil || !parser.IsName(pe.Param.Value) || p.dialect == dialect.Zsh {
		return
	}
	next, ok := nextPart(p.parent(), pe)
	if !ok {
		return
	}
	if lit, ok := next.(*syntax.Lit); !ok || !strings.HasPrefix(lit.Value, "[") {
		return
	}
	span, ok := p.span(pe)
	if !ok {
		return
	}
	name := pe.Param.Value
	p.report(1087, span, "$"+name+"[ expands $"+name+" and appends a literal [; write ${"+name+"[...]} to index the array.")
}

func nextPart(parent syntax.Node, part syntax.WordPart) (syntax.WordPart, bool) {
	var parts []syntax.WordPart
	switch parent := parent.(type) {
	case *syntax.Word:
		parts = parent.Parts
	case *syntax.DblQuoted:
		parts = parent.Parts
	default:
		return nil, false
	}
	for i, candidate := range parts[:max(0, len(parts)-1)] {
		if candidate == part {
			return parts[i+1], true
		}
	}
	return nil, false
}

func checkBackgroundAmpersand(p *pass, n syntax.Node) {
	stmt, ok := n.(*syntax.Stmt)
	if !ok || !stmt.Background || !stmt.Semicolon.IsValid() {
		return
	}
	at := parser.Offset(stmt.Semicolon)
	if at+1 >= len(p.text) || p.text[at] != '&' {
		return
	}
	rest := p.text[at+1:]
	switch {
	case strings.HasPrefix(rest, "amp;"), strings.HasPrefix(rest, "gt;"), strings.HasPrefix(rest, "lt;"):
		p.report(1109, parser.Span{Start: at, End: at + 1 + strings.IndexByte(rest, ';') + 1}, htmlEntityMessage)
	case rest[0] == '_' || isASCIILetter(rest[0]):
		p.report(1132, parser.Span{Start: at, End: at + 1}, "This & runs the command in the background and starts a new one; quote the word if the & is part of it, such as in a URL.")
	}
}

func checkQuotedDollar(p *pass, n syntax.Node) {
	dq, ok := n.(*syntax.DblQuoted)
	if !ok || dq.Dollar || len(dq.Parts) == 0 {
		return
	}
	last, ok := dq.Parts[len(dq.Parts)-1].(*syntax.Lit)
	if !ok || !strings.HasSuffix(last.Value, "$") || escaped(last.Value, len(last.Value)-1) {
		return
	}
	next, ok := nextPart(p.parent(), dq)
	if !ok || !startsParameter(next) {
		return
	}
	span, ok := p.span(last)
	if !ok {
		return
	}
	p.report(1135, parser.Span{Start: span.End - 1, End: span.End}, "Ending the quotes right after $ only to keep it literal is confusing; escape it as \\$ inside the quotes instead.")
}

func startsParameter(part syntax.WordPart) bool {
	if dq, ok := part.(*syntax.DblQuoted); ok && !dq.Dollar && len(dq.Parts) > 0 {
		part = dq.Parts[0]
	}
	lit, ok := part.(*syntax.Lit)
	if !ok || lit.Value == "" {
		return false
	}
	c := lit.Value[0]
	return parser.IsNameChar(c) || strings.IndexByte("@#?!-", c) >= 0
}

func checkCommentedContinuation(p *pass, n syntax.Node) {
	call, ok := n.(*syntax.CallExpr)
	if !ok {
		return
	}
	for i, arg := range call.Args {
		span, ok := p.span(arg)
		if !ok {
			continue
		}
		end := len(p.text)
		if i+1 < len(call.Args) {
			end = parser.Offset(call.Args[i+1].Pos())
		}
		if span.End <= end {
			p.checkContinuedComment(span.End, end)
		}
	}
}

func (p *pass) checkContinuedComment(start, end int) {
	gap := p.text[start:end]
	lineEnd := strings.IndexByte(gap, '\n')
	if lineEnd < 2 || gap[lineEnd-1] != '\\' || strings.TrimLeft(gap[:lineEnd-1], " \t") != "" || gap[:lineEnd-1] == "" {
		return
	}
	comment := strings.TrimLeft(gap[lineEnd+1:], " \t")
	commentEnd := strings.IndexByte(comment, '\n')
	if !strings.HasPrefix(comment, "#") || commentEnd < 1 || comment[commentEnd-1] != '\\' {
		return
	}
	at := end - len(comment) + commentEnd
	p.report(1143, parser.Span{Start: at, End: at}, "This commented-out line ends the continued command: a comment cannot continue a line, so the lines below run as a separate command.")
}
