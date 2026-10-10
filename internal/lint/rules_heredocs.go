package lint

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
)

var heredocsFamily = family{
	codes: map[Code]meta{
		1039: {severity: SeverityError},
		1040: {severity: SeverityError},
	},
	rules: []rule{
		{finish: checkIndentedHeredocEnds},
	},
}

func checkIndentedHeredocEnds(p *pass) {
	for _, h := range p.tree.Heredocs {
		token := strings.TrimLeft(h.Delimiter, " \t")
		start := h.Body.Start
		for line := range strings.Lines(p.text[h.Body.Start:h.Body.End]) {
			p.indentedHeredocEnd(h, token, start, strings.TrimSuffix(line, "\n"))
			start += len(line)
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
	case !h.StripTabs:
		p.report(1039, at, "This line is not the end of the here-document because "+h.Delimiter+" is indented; remove the indentation, or use <<- and indent with tabs only.")
	case strings.Contains(indent, " "):
		p.report(1040, at, "This line is not the end of the here-document because <<- strips only leading tabs, not spaces; indent "+h.Delimiter+" with tabs.")
	}
}
