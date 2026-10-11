package diagnostics

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/bats"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

const BatsSource = "bats"

func Bats(failures []bats.Failure, doc *docstore.Document, enc docstore.Encoding) []lsp.Diagnostic {
	out := make([]lsp.Diagnostic, len(failures))
	text := doc.Text()
	for i, f := range failures {
		start, end := doc.LineBounds(f.Line - 1)
		start += len(text[start:end]) - len(strings.TrimLeft(text[start:end], " \t"))
		out[i] = lsp.Diagnostic{
			Range:    doc.RangeFor(start, end, enc),
			Severity: lsp.SeverityError,
			Source:   BatsSource,
			Message:  f.Message,
		}
	}
	return out
}
