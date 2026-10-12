package server

import (
	"time"

	"github.com/stubbedev/shell-lsp/internal/regularfile"
	"github.com/stubbedev/shell-lsp/internal/shellcheckrc"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

func newShellcheckrcResolver(fallback []string) *shellcheckrc.Resolver {
	return shellcheckrc.NewResolver(regularfile.ReadConfig, time.Now, workspace.DiskRecheck, fallback)
}
