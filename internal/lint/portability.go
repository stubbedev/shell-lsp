package lint

import (
	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

var (
	strictShells = dialect.SetOf(dialect.Posix, dialect.Dash, dialect.Busybox)
	posixOrDash  = dialect.SetOf(dialect.Posix, dialect.Dash)
	posixOnly    = dialect.SetOf(dialect.Posix)
	ashShells    = dialect.SetOf(dialect.Dash, dialect.Busybox)
)

type portabilityIssue struct {
	code        Code
	shells      dialect.Set
	feature     string
	alternative string
}

func (p *pass) nonPortable(issue portabilityIssue, span parser.Span) {
	if !issue.shells.Has(p.dialect) {
		return
	}
	severity, message := SeverityError, issue.feature+" is not supported in "+shellLabel(p.dialect)+"."
	if p.dialect == dialect.Posix {
		severity, message = SeverityWarning, issue.feature+" is undefined in POSIX sh."
	}
	if issue.alternative != "" {
		message += " Use " + issue.alternative + " instead."
	}
	p.reportWithSeverity(issue.code, severity, span, message)
}

func (p *pass) nonPortableNode(issue portabilityIssue, n syntax.Node) {
	if !issue.shells.Has(p.dialect) {
		return
	}
	if span, ok := p.span(n); ok {
		p.nonPortable(issue, span)
	}
}

func shellLabel(d dialect.Dialect) string {
	if d == dialect.Busybox {
		return "busybox sh"
	}
	return d.String()
}

func posSpan(pos syntax.Pos, length int) parser.Span {
	start := parser.Offset(pos)
	return parser.Span{Start: start, End: start + length}
}
