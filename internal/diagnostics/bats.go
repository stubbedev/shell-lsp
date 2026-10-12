package diagnostics

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/bats"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

const BatsSource = "bats"

func Bats(failures []bats.Failure, at docstore.Positions) []lsp.Diagnostic {
	doc := at.Document()
	out := make([]lsp.Diagnostic, len(failures))
	text := doc.Text()
	for i, f := range failures {
		start, end := doc.LineBounds(f.Line - 1)
		start += len(text[start:end]) - len(strings.TrimLeft(text[start:end], " \t"))
		out[i] = lsp.Diagnostic{
			Range:    at.Range(parser.Span{Start: start, End: end}),
			Severity: lsp.SeverityError,
			Source:   BatsSource,
			Message:  f.Message,
		}
	}
	return out
}
