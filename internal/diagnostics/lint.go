package diagnostics

import (
	"fmt"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

func severity(s lint.Severity) lsp.DiagnosticSeverity {
	switch s {
	case lint.SeverityError:
		return lsp.SeverityError
	case lint.SeverityWarning:
		return lsp.SeverityWarning
	case lint.SeverityInfo:
		return lsp.SeverityInformation
	case lint.SeverityStyle:
		return lsp.SeverityHint
	}
	panic(fmt.Sprintf("diagnostics: lint severity %d has no LSP severity", s))
}

var tags = map[lint.Tag]lsp.DiagnosticTag{
	lint.TagUnnecessary: lsp.TagUnnecessary,
	lint.TagDeprecated:  lsp.TagDeprecated,
}

func Lint(findings []lint.Finding, at docstore.Positions, codeDescriptions bool) []lsp.Diagnostic {
	out := make([]lsp.Diagnostic, 0, len(findings))
	for _, f := range findings {
		d := lsp.Diagnostic{
			Range:    at.Range(f.Span),
			Severity: severity(f.Severity),
			Code:     f.Code.String(),
			Source:   Source,
			Message:  f.Message,
		}
		if codeDescriptions {
			d.CodeDescription = &lsp.CodeDescription{Href: f.Code.Docs()}
		}
		if tag, ok := tags[f.Tag]; ok {
			d.Tags = []lsp.DiagnosticTag{tag}
		}
		out = append(out, d)
	}
	return out
}
