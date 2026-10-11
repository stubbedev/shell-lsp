package server

import (
	"os"
	"time"

	"github.com/stubbedev/shell-lsp/internal/regularfile"
	"github.com/stubbedev/shell-lsp/internal/shellcheckrc"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

func newShellcheckrcResolver(fallback []string) *shellcheckrc.Resolver {
	return shellcheckrc.NewResolver(regularfile.ReadConfig, time.Now, workspace.DiskRecheck, fallback)
}

func userShellcheckrcs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return shellcheckrc.Fallback(home, os.Getenv("XDG_CONFIG_HOME"))
}
