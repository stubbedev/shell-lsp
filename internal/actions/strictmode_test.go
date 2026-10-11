package actions

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

func TestStrictMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		d                dialect.Dialect
		src, want, title string
	}{
		{dialect.Bash, "#!/bin/bash\n«»echo hi\n", "#!/bin/bash\nset -euo pipefail\n\necho hi\n", "Add set -euo pipefail"},
		{dialect.Bash, "#!/bin/bash\n# Deploys the site.\n#\n# shellcheck disable=SC2034\n\necho «hi»\n", "#!/bin/bash\n# Deploys the site.\n#\n# shellcheck disable=SC2034\nset -euo pipefail\n\necho hi\n", "Add set -euo pipefail"},
		{dialect.Zsh, "#!/bin/zsh\n«»", "#!/bin/zsh\nset -euo pipefail\n", "Add set -euo pipefail"},
		{dialect.Ksh, "#!/bin/ksh«»", "#!/bin/ksh\nset -euo pipefail\n", "Add set -euo pipefail"},
		{dialect.Posix, "#!/bin/sh\n# comment\n«»\nmain\n", "#!/bin/sh\n# comment\nset -eu\n\nmain\n", "Add set -eu"},
		{dialect.Dash, "#!/bin/dash\n«»echo\n", "#!/bin/dash\nset -eu\n\necho\n", "Add set -eu"},
		{dialect.Bash, "#!/bin/bash\nset -e\n«»echo\n", "#!/bin/bash\nset -euo pipefail\n\nset -e\necho\n", "Add set -euo pipefail"},
		{dialect.Bash, "#!/bin/bash -eu\n«»echo\n", "#!/bin/bash -eu\nset -euo pipefail\n\necho\n", "Add set -euo pipefail"},
		{dialect.Posix, "#!/bin/sh\nset -o pipefail -e\n«»echo\n", "#!/bin/sh\nset -eu\n\nset -o pipefail -e\necho\n", "Add set -eu"},
	}
	for _, tc := range tests {
		got, title, ok := refactorIn(t, tc.d, StrictMode, tc.src)
		if !ok || got != tc.want || title != tc.title {
			t.Errorf("%s: strict mode %q = %q (%q, %t), want %q (%q)", tc.d, tc.src, got, title, ok, tc.want, tc.title)
		}
	}
	for _, tc := range []struct {
		d   dialect.Dialect
		src string
	}{
		{dialect.Bash, "#!/bin/bash\nset -euo pipefail\n«»echo\n"},
		{dialect.Bash, "#!/bin/bash\nset -o errexit -o nounset -o pipefail\n«»echo\n"},
		{dialect.Bash, "#!/bin/bash -e\nset -u -o pipefail\n«»echo\n"},
		{dialect.Bash, "#!/bin/bash\nmain() {\n\tset -eu\n\tset -o pipefail\n}\n«»"},
		{dialect.Posix, "#!/bin/sh\nset -eu\n«»echo\n"},
		{dialect.Bash, "«»echo hi\n"},
		{dialect.Bash, "#!/usr/bin/env bats\n«»@test \"x\" {\n\ttrue\n}\n"},
	} {
		if got, _, ok := refactorIn(t, tc.d, StrictMode, tc.src); ok {
			t.Errorf("%s: strict mode %q offered: %q", tc.d, tc.src, got)
		}
	}
}
