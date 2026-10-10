package diagnostics

import (
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

const Source = "shell-lsp"

const (
	CodeSyntax  = "syntax"
	CodeDialect = "dialect"
)

func Syntax(tree *parser.Tree, doc *docstore.Document, enc docstore.Encoding) []lsp.Diagnostic {
	out := make([]lsp.Diagnostic, 0, len(tree.Errors))
	for _, e := range tree.Errors {
		code := CodeSyntax
		if e.Kind == parser.ErrDialect {
			code = CodeDialect
		}
		out = append(out, lsp.Diagnostic{
			Range:    doc.RangeFor(e.Span.Start, e.Span.End, enc),
			Severity: lsp.SeverityError,
			Code:     code,
			Source:   Source,
			Message:  e.Message,
		})
	}
	return out
}
