package server

import (
	"github.com/stubbedev/shell-lsp/internal/embedded"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

func embeddedPrograms(file *workspace.File) []embedded.Program {
	return workspace.Memo(file, "embedded.programs", 0, func() []embedded.Program { return embedded.Find(file.Tree) })
}
