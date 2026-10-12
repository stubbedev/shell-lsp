package lint

import (
	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var (
	strictShells = dialect.SetOf(dialect.Posix, dialect.Dash, dialect.Busybox)
	posixOrDash  = dialect.SetOf(dialect.Posix, dialect.Dash)
	posixOnly    = dialect.SetOf(dialect.Posix)
	ashShells    = dialect.SetOf(dialect.Dash, dialect.Busybox)
)

type portabilityIssue struct {
	code        Code
	feature     string
	alternative string
	replacement string
}

func (p *pass) replacementFix(issue portabilityIssue, span parser.Span) []Fix {
	if issue.replacement == "" {
		return nil
	}
	return []Fix{safeFix("Replace "+p.text[span.Start:span.End]+" with "+issue.replacement, textedit.Edit{Start: span.Start, End: span.End, NewText: issue.replacement})}
}

func (p *pass) nonPortable(issue portabilityIssue, span parser.Span, fixes ...Fix) {
	if !p.meta(issue.code).shells.Has(p.dialect) {
		return
	}
	severity, message := SeverityError, issue.feature+" is not supported in "+shellLabel(p.dialect)+"."
	if p.dialect == dialect.Posix {
		severity, message = SeverityWarning, issue.feature+" is undefined in POSIX sh."
	}
	if issue.alternative != "" {
		message += " Use " + issue.alternative + " instead."
	}
	p.reportWithSeverity(issue.code, severity, span, message, fixes...)
}

func (p *pass) nonPortableNode(issue portabilityIssue, n syntax.Node, fixes ...Fix) {
	if !p.meta(issue.code).shells.Has(p.dialect) {
		return
	}
	if span, ok := p.span(n); ok {
		p.nonPortable(issue, span, fixes...)
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
