package server

import (
	"github.com/stubbedev/shell-lsp/internal/embedded"
	"github.com/stubbedev/shell-lsp/internal/hostnav"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

func embeddedPrograms(file *workspace.File) []embedded.Program { return hostnav.Programs(file) }
