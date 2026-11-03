package server

import (
	"github.com/stubbedev/shell-lsp/internal/embedded"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

func embeddedPrograms(file *workspace.File) []embedded.Program { return navigation.Programs(file) }
