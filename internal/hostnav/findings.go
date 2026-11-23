package hostnav

import (
	"slices"

	"github.com/stubbedev/shell-lsp/internal/filelint"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

func HostFindings(h Host, own, locked []lint.Finding, docker Hadolint, settings filelint.Settings) []lint.Finding {
	excluded := settings.Excluded()
	return slices.DeleteFunc(slices.Concat(own, OptionalNixFindings(h, settings), locked, docker.Findings()), func(f lint.Finding) bool {
		return excluded.Has(f.Code) || !settings.Profile.KeepsHost(f)
	})
}
