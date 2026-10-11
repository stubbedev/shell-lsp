package directive

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func TestHeaderLinesStopAtTheFirstCommand(t *testing.T) {
	t.Parallel()

	text := "#!/bin/sh\n\n# shellcheck disable=SC2086\n# note\n# shellcheck shell=dash\necho\n# shellcheck disable=SC2034\n"
	lines := HeaderLines(text)
	if len(lines) != 2 || lines[0].Start != 11 || lines[1].Next != len("#!/bin/sh\n\n# shellcheck disable=SC2086\n# note\n# shellcheck shell=dash\n") {
		t.Fatalf("HeaderLines = %+v", lines)
	}
	if p, ok := lines[1].Pair(Shell); !ok || p.Value != "dash" {
		t.Errorf("Pair(shell) = %+v, %t", p, ok)
	}
}

func TestHeaderInsert(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ text, want string }{
		{"echo\n", "# shellcheck disable=SC2086\necho\n"},
		{"#!/bin/sh", "#!/bin/sh\n# shellcheck disable=SC2086\n"},
		{"#!/bin/sh\n# shellcheck shell=dash\n# license\necho\n", "#!/bin/sh\n# shellcheck shell=dash\n# shellcheck disable=SC2086\n# license\necho\n"},
		{"", "# shellcheck disable=SC2086\n"},
	} {
		if got := textedit.Apply(tc.text, []textedit.Edit{HeaderInsert(tc.text, "disable=SC2086")}); got != tc.want {
			t.Errorf("HeaderInsert(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
}

func TestSettingsLinesSkipCommentsAndBrokenLines(t *testing.T) {
	t.Parallel()

	lines := SettingsLines("# disable=SC1000\ndisable=SC2086\nnot a setting\nshell=bash\n")
	if len(lines) != 2 || lines[0].Start != len("# disable=SC1000\n") {
		t.Errorf("SettingsLines = %+v", lines)
	}
}
