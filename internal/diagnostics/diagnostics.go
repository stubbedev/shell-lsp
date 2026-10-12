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

func Syntax(tree *parser.Tree, at docstore.Positions) []lsp.Diagnostic {
	out := make([]lsp.Diagnostic, 0, len(tree.Errors))
	for _, e := range tree.Errors {
		code := CodeSyntax
		if e.Kind == parser.ErrDialect {
			code = CodeDialect
		}
		out = append(out, lsp.Diagnostic{
			Range:    at.Range(e.Span),
			Severity: lsp.SeverityError,
			Code:     code,
			Source:   Source,
			Message:  e.Message,
		})
	}
	return out
}
