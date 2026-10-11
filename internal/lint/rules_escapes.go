package lint

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var escapesFamily = family{
	codes: map[Code]meta{
		1001: {severity: SeverityInfo, summary: "A backslash before an ordinary character outside quotes does nothing."},
		1003: {severity: SeverityInfo, bug: true, summary: "A backslash cannot escape a single quote inside single quotes, so the quote ends the string."},
		1011: {severity: SeverityWarning, summary: "An apostrophe inside single quotes closes the string, leaving the rest outside the quotes."},
		1012: {severity: SeverityWarning, summary: "An escape like \\n or \\t outside quotes is just the letter, not a control character."},
	},
	rules: []rule{
		{on: nodes(&syntax.Lit{}), node: checkUnquotedEscapes},
		{on: nodes(&syntax.SglQuoted{}), node: checkSingleQuoteEnding},
	},
}

const meaningfulEscapes = " \t\n\r|&;<>()\\'\"$`?*@!+[]{}.,~#\u00a0"

var controlEscapes = map[rune]string{
	'n': "`\\n` outside quotes is just the letter n; to embed a line feed, put a literal line break inside quotes.",
	't': "`\\t` outside quotes is just the letter t; to embed a tab, use \"$(printf '\\t')\".",
	'r': "`\\r` outside quotes is just the letter r; to embed a carriage return, use \"$(printf '\\r')\".",
}

func checkUnquotedEscapes(p *pass, n syntax.Node) {
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
	aliasEscape := word.Parts[0] == lit && p.isCommandName(word)
	raw := p.text[span.Start:span.End]
	for i := 0; i+1 < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		r, size := utf8.DecodeRuneInString(raw[i+1:])
		escape := parser.Span{Start: span.Start + i, End: span.Start + i + 1 + size}
		i += size
		switch message, control := controlEscapes[r]; {
		case escape.Start == span.Start && aliasEscape && isAliasEscaped(r):
		case control:
			p.report(1012, escape, message)
		case strings.ContainsRune(meaningfulEscapes, r):
		default:
			p.report(1001, escape, "This backslash does nothing: outside quotes \\"+string(r)+" is just "+string(r)+".",
				safeFix("Remove the backslash", textedit.Edit{Start: escape.Start, End: escape.Start + 1}))
		}
	}
}

func (p *pass) isCommandName(word *syntax.Word) bool {
	call, ok := p.ancestor(1).(*syntax.CallExpr)
	return ok && len(call.Args) > 0 && call.Args[0] == word
}

func isAliasEscaped(r rune) bool {
	return 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' || r == '_' || r == ':' || r == '.'
}

func checkSingleQuoteEnding(p *pass, n syntax.Node) {
	sq, ok := n.(*syntax.SglQuoted)
	if !ok || sq.Dollar || !sq.Right.IsValid() {
		return
	}
	closing := parser.Offset(sq.Right)
	if strings.HasSuffix(sq.Value, `\`) {
		p.report(1003, parser.Span{Start: closing - 1, End: closing + 1}, `A backslash does not escape inside single quotes, so this quote ends the string; to embed a single quote write '\'' (close, escaped quote, reopen).`)
		return
	}
	last, _ := utf8.DecodeLastRuneInString(sq.Value)
	if !unicode.IsLetter(last) {
		return
	}
	next, ok := p.nextWordPart(sq).(*syntax.Lit)
	if !ok || parser.Offset(next.ValuePos) != closing+1 {
		return
	}
	if first, _ := utf8.DecodeRuneInString(next.Value); unicode.IsLetter(first) {
		p.report(1011, parser.Span{Start: closing, End: closing + 1}, "This apostrophe closes the single-quoted string, so "+next.Value+" is outside the quotes; write '\\'' for a literal apostrophe or use double quotes.")
	}
}

func (p *pass) nextWordPart(part syntax.WordPart) syntax.WordPart {
	word, ok := p.parent().(*syntax.Word)
	if !ok {
		return nil
	}
	for i, candidate := range word.Parts[:len(word.Parts)-1] {
		if candidate == part {
			return word.Parts[i+1]
		}
	}
	return nil
}
