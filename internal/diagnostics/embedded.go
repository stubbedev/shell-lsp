package diagnostics

import (
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/embedded"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

func Embedded(programs []embedded.Program, doc *docstore.Document, enc docstore.Encoding) []lsp.Diagnostic {
	var out []lsp.Diagnostic
	for i := range programs {
		p := &programs[i]
		err := p.Result.Error
		if err == nil {
			continue
		}
		span := p.Span(err.Start, err.End)
		out = append(out, lsp.Diagnostic{
			Range:    doc.RangeFor(span.Start, span.End, enc),
			Severity: lsp.SeverityError,
			Code:     p.Language.Code(),
			Source:   Source,
			Message:  p.Language.String() + ": " + err.Message,
		})
	}
	return out
}
