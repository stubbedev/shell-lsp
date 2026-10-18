package server

import (
	"time"

	"github.com/stubbedev/shell-lsp/internal/hadolint"
	"github.com/stubbedev/shell-lsp/internal/regularfile"
	"github.com/stubbedev/shell-lsp/internal/shellcheckrc"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

func newShellcheckrcResolver(fallback []string) *shellcheckrc.Resolver {
	return shellcheckrc.NewResolver(regularfile.ReadConfig, time.Now, workspace.DiskRecheck, fallback)
}

func newHadolintResolver(fallback []string) *hadolint.Resolver {
	return hadolint.NewResolver(regularfile.ReadConfig, time.Now, workspace.DiskRecheck, fallback)
}
