package lint

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

var negationFamily = family{
	codes: map[Code]meta{
		2325: {severity: SeverityError, summary: "sh allows only one ! before a pipeline."},
		2326: {severity: SeverityError, summary: "! cannot negate a single stage after a |."},
	},
	finish: []func(*pass){checkRejectedNegations},
}

var negatedStageShells = dialect.SetOf(dialect.Bash, dialect.Posix, dialect.Dash, dialect.Busybox)

func checkRejectedNegations(p *pass) {
	if !negatedStageShells.Has(p.dialect) {
		return
	}
	for _, e := range p.tree.Errors {
		at := e.Span.Start
		if at >= len(p.text) || p.text[at] != '!' {
			continue
		}
		bang := parser.Span{Start: at, End: at + 1}
		switch {
		case strings.HasPrefix(strings.TrimLeft(p.text[at+1:], " \t"), "!"):
			if strictShells.Has(p.dialect) {
				p.report(2325, bang, "Only one ! may precede a pipeline in sh; stacking them is a bash/ksh extension. Drop both, or keep one.")
			}
		case strings.HasSuffix(strings.TrimRight(p.text[:at], " \t"), "|"):
			p.report(2326, bang, "! cannot negate a single stage after |; wrap that command in a group, like | { ! cmd; }, or negate the whole pipeline.")
		}
	}
}
