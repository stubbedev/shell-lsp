package diagnostics

import (
	"strconv"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

const referenceURL = "https://www.shellcheck.net/wiki/SC"

var severities = map[lint.Severity]lsp.DiagnosticSeverity{
	lint.SeverityError:   lsp.SeverityError,
	lint.SeverityWarning: lsp.SeverityWarning,
	lint.SeverityInfo:    lsp.SeverityInformation,
	lint.SeverityStyle:   lsp.SeverityHint,
}

var tags = map[lint.Tag]lsp.DiagnosticTag{
	lint.TagUnnecessary: lsp.TagUnnecessary,
	lint.TagDeprecated:  lsp.TagDeprecated,
}

func Lint(findings []lint.Finding, doc *docstore.Document, enc docstore.Encoding) []lsp.Diagnostic {
	out := make([]lsp.Diagnostic, 0, len(findings))
	for _, f := range findings {
		d := lsp.Diagnostic{
			Range:           doc.RangeFor(f.Span.Start, f.Span.End, enc),
			Severity:        severities[f.Severity],
			Code:            f.Code.String(),
			CodeDescription: &lsp.CodeDescription{Href: referenceURL + strconv.Itoa(int(f.Code))},
			Source:          Source,
			Message:         f.Message,
		}
		if tag, ok := tags[f.Tag]; ok {
			d.Tags = []lsp.DiagnosticTag{tag}
		}
		out = append(out, d)
	}
	return out
}
