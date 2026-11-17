package server

import (
	"fmt"

	"github.com/stubbedev/shell-lsp/internal/embedded"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

func embeddedPrograms(file *workspace.File) []embedded.Program { return navigation.Programs(file) }

func embeddedCompletionKind(kind embedded.ItemKind) lsp.CompletionItemKind {
	switch kind {
	case embedded.FunctionItem:
		return lsp.CompletionFunction
	case embedded.VariableItem:
		return lsp.CompletionVariable
	case embedded.CommandItem:
		return lsp.CompletionKeyword
	case embedded.OperatorItem:
		return lsp.CompletionOperator
	case embedded.LabelItem:
		return lsp.CompletionReference
	}
	panic(fmt.Sprintf("server: embedded completion kind %d has no LSP completion kind", kind))
}
