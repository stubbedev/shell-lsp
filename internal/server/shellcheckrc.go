package server

import (
	"time"

	"github.com/stubbedev/shell-lsp/internal/hadolint"
	"github.com/stubbedev/shell-lsp/internal/host/nix"
	"github.com/stubbedev/shell-lsp/internal/regularfile"
	"github.com/stubbedev/shell-lsp/internal/shellcheckrc"
	"github.com/stubbedev/shell-lsp/internal/statixconfig"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

func newShellcheckrcResolver(fallback []string) *shellcheckrc.Resolver {
	return shellcheckrc.NewResolver(regularfile.ReadConfig, time.Now, workspace.DiskRecheck, fallback)
}

func newStatixResolver() *statixconfig.Resolver {
	return statixconfig.NewResolver(regularfile.ReadConfig, time.Now, workspace.DiskRecheck)
}

func newFlakeLocks() *nix.LockFiles {
	return nix.NewLockFiles(regularfile.ReadConfig, time.Now, workspace.DiskRecheck)
}

func newHadolintResolver(fallback []string) *hadolint.Resolver {
	return hadolint.NewResolver(regularfile.ReadConfig, time.Now, workspace.DiskRecheck, fallback)
}
