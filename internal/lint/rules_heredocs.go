package lint

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
)

var heredocsFamily = family{
	codes: map[Code]meta{
		1039: {severity: SeverityError, summary: "An indented terminator does not end the here-document."},
		1040: {severity: SeverityError, summary: "A terminator indented with spaces does not end a <<- here-document, which strips only tabs."},
		1118: {severity: SeverityError, summary: "A here-document terminator followed by whitespace does not end the here-document."},
		1119: {severity: SeverityError, summary: "A here-document terminator followed by ) does not end the here-document."},
		1120: {severity: SeverityError, summary: "A here-document terminator followed by a comment does not end the here-document."},
		1121: {severity: SeverityError, summary: "A here-document terminator followed by an operator does not end the here-document."},
		1122: {severity: SeverityError, summary: "A here-document terminator followed by more text does not end the here-document."},
	},
	finish: []func(*pass){checkHeredocEnds},
}

var endTokenCodes = map[parser.EndTokenMistake]Code{
	parser.TrailingWhitespace: 1118,
	parser.ClosingParenthesis: 1119,
	parser.TrailingComment:    1120,
	parser.TrailingOperator:   1121,
	parser.TrailingText:       1122,
}

func checkHeredocEnds(p *pass) {
	for _, h := range p.tree.Heredocs {
		token := strings.TrimLeft(h.Delimiter, " \t")
		start := h.Body.Start
		for line := range strings.Lines(p.text[h.Body.Start:h.Body.End]) {
			line = strings.TrimSuffix(line, "\n")
			p.indentedHeredocEnd(h, token, start, line)
			p.nearMissHeredocEnd(h, start, line)
			start += len(line) + 1
		}
	}
}

func (p *pass) indentedHeredocEnd(h parser.Heredoc, token string, start int, line string) {
	content := strings.TrimLeft(line, " \t")
	indent := line[:len(line)-len(content)]
	if indent == "" || strings.TrimRight(content, " \t\r") != token {
		return
	}
	at := parser.Span{Start: start, End: start + len(line)}
	switch {
	case h.Strip == parser.StripNone:
		p.report(1039, at, "This line is not the end of the here-document because "+h.Delimiter+" is indented; remove the indentation, or use <<- and indent with tabs only.")
	case h.Strip == parser.StripTabs && strings.Contains(indent, " "):
		p.report(1040, at, "This line is not the end of the here-document because <<- strips only leading tabs, not spaces; indent "+h.Delimiter+" with tabs.")
	}
}

func (p *pass) nearMissHeredocEnd(h parser.Heredoc, start int, line string) {
	indent := 0
	switch h.Strip {
	case parser.StripTabs:
		indent = len(line) - len(strings.TrimLeft(line, "\t"))
	case parser.StripBlanks:
		indent = len(line) - len(strings.TrimLeft(line, " \t"))
	}
	mistake, at, ok := parser.NearMissEndToken(line[indent:], h.Delimiter)
	if !ok {
		return
	}
	span := parser.Span{Start: start + indent + at, End: start + len(line)}
	p.report(endTokenCodes[mistake], span, "This line does not end the here-document: "+h.Delimiter+" "+mistake.String()+". The end token must stand alone on its line.")
}
