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
		{dialect.Bash, "#!/bin/bash\nset -e\n«»echo\n", "#!/bin/bash\nset -euo pipefail\necho\n", "Change set -e to set -euo pipefail"},
		{dialect.Bash, "#!/bin/bash\nset -ex # trace\n«»echo\n", "#!/bin/bash\nset -exuo pipefail # trace\necho\n", "Change set -ex to set -exuo pipefail"},
		{dialect.Bash, "#!/bin/bash\nset -e -x\n«»echo\n", "#!/bin/bash\nset -eu -x -o pipefail\necho\n", "Change set -e -x to set -eu -x -o pipefail"},
		{dialect.Bash, "#!/bin/bash\nset -u; set -o pipefail\n«»echo\n", "#!/bin/bash\nset -ue; set -o pipefail\necho\n", "Change set -u to set -ue"},
		{dialect.Bash, "#!/bin/bash\nset -o errexit\n«»echo\n", "#!/bin/bash\nset -o errexit -o nounset -o pipefail\necho\n", "Change set -o errexit to set -o errexit -o nounset -o pipefail"},
		{dialect.Ksh, "#!/bin/ksh\nset -o nounset -e\n«»echo\n", "#!/bin/ksh\nset -o nounset -eo pipefail\necho\n", "Change set -o nounset -e to set -o nounset -eo pipefail"},
		{dialect.Bash, "#!/bin/bash -eu\n«»echo\n", "#!/bin/bash -eu\nset -o pipefail\n\necho\n", "Add set -o pipefail"},
		{dialect.Bash, "#!/bin/bash\nf() {\n\tset -e\n}\n«»", "#!/bin/bash\nset -uo pipefail\n\nf() {\n\tset -e\n}\n", "Add set -uo pipefail"},
		{dialect.Bash, "#!/bin/bash\nset -e -- \"$@\"\n«»", "#!/bin/bash\nset -uo pipefail\n\nset -e -- \"$@\"\n", "Add set -uo pipefail"},
		{dialect.Bash, "#!/bin/bash\nset -e >/dev/null\n«»", "#!/bin/bash\nset -uo pipefail\n\nset -e >/dev/null\n", "Add set -uo pipefail"},
		{dialect.Posix, "#!/bin/sh\nset -o pipefail -e\n«»echo\n", "#!/bin/sh\nset -o pipefail -eu\necho\n", "Change set -o pipefail -e to set -o pipefail -eu"},
		{dialect.Zsh, "#!/bin/zsh\nsetopt ERR_EXIT\n«»echo\n", "#!/bin/zsh\nsetopt ERR_EXIT no_unset pipe_fail\necho\n", "Change setopt ERR_EXIT to setopt ERR_EXIT no_unset pipe_fail"},
		{dialect.Zsh, "#!/bin/zsh\nsetopt extended_glob pipefail\n«»echo\n", "#!/bin/zsh\nsetopt extended_glob pipefail err_exit no_unset\necho\n", "Change setopt extended_glob pipefail to setopt extended_glob pipefail err_exit no_unset"},
		{dialect.Zsh, "#!/bin/zsh\nunsetopt unset\n«»echo\n", "#!/bin/zsh\nset -eo pipefail\n\nunsetopt unset\necho\n", "Add set -eo pipefail"},
		{dialect.Zsh, "#!/bin/zsh\nunsetopt errexit\n«»echo\n", "#!/bin/zsh\nset -euo pipefail\n\nunsetopt errexit\necho\n", "Add set -euo pipefail"},
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
		{dialect.Ksh, "#!/bin/ksh\nset -o errexit\nset -o nounset\nset -o pipefail\n«»echo\n"},
		{dialect.Zsh, "#!/bin/zsh\nsetopt err_exit no_unset pipe_fail\n«»echo\n"},
		{dialect.Zsh, "#!/bin/zsh\nsetopt errexit nounset PIPEFAIL\n«»echo\n"},
		{dialect.Zsh, "#!/bin/zsh\nsetopt ERR_EXIT NO_UNSET\nset -o pipefail\n«»echo\n"},
		{dialect.Zsh, "#!/bin/zsh\nsetopt err_exit pipe_fail\nunsetopt unset\n«»echo\n"},
		{dialect.Bash, "«»echo hi\n"},
		{dialect.Bash, "#!/usr/bin/env bats\n«»@test \"x\" {\n\ttrue\n}\n"},
	} {
		if got, _, ok := refactorIn(t, tc.d, StrictMode, tc.src); ok {
			t.Errorf("%s: strict mode %q offered: %q", tc.d, tc.src, got)
		}
	}
}
