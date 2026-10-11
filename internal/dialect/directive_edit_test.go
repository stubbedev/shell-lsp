package dialect

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func TestShellDirectiveEdits(t *testing.T) {
	t.Parallel()

	dash, zsh, bash := Dash, Zsh, Bash
	for _, c := range []struct {
		name  string
		text  string
		shell *Dialect
		want  string
	}{
		{"inserted after the shebang", "#!/bin/sh\necho hi\n", &dash, "#!/bin/sh\n# shellcheck shell=dash\necho hi\n"},
		{"inserted after a lone shebang", "#!/bin/sh", &dash, "#!/bin/sh\n# shellcheck shell=dash\n"},
		{"inserted at the top without a shebang", "echo hi\n", &zsh, "# shellcheck shell=zsh\necho hi\n"},
		{"value replaced in place", "#!/bin/sh\n# shellcheck shell=sh disable=SC2086\necho\n", &bash, "#!/bin/sh\n# shellcheck shell=bash disable=SC2086\necho\n"},
		{"quoted value replaced", "# shellcheck shell=\"sh\"\necho\n", &bash, "# shellcheck shell=bash\necho\n"},
		{"same shell left alone", "# shellcheck shell=bash\necho\n", &bash, "# shellcheck shell=bash\necho\n"},
		{"lone directive removed with its line", "#!/bin/sh\n# shellcheck shell=dash\necho\n", nil, "#!/bin/sh\necho\n"},
		{"first pair removed", "# shellcheck shell=dash disable=SC2086\necho\n", nil, "# shellcheck disable=SC2086\necho\n"},
		{"last pair removed", "# shellcheck disable=SC2086 shell=dash\necho\n", nil, "# shellcheck disable=SC2086\necho\n"},
		{"nothing to remove", "#!/bin/sh\necho\n", nil, "#!/bin/sh\necho\n"},
		{"directive after the first command is not the header one", "echo\n# shellcheck shell=dash\n", &zsh, "# shellcheck shell=zsh\necho\n# shellcheck shell=dash\n"},
	} {
		got := textedit.Apply(c.text, ShellDirectiveEdits(c.text, c.shell))
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
		if c.shell != nil {
			if d := Resolve(nil, Input{Text: got}); d.Dialect != *c.shell || d.Source != SourceDirective {
				t.Errorf("%s: the edited text resolves to %+v, want %s from the directive", c.name, d, c.shell)
			}
		}
	}
}

func TestHasShellDirective(t *testing.T) {
	t.Parallel()

	for text, want := range map[string]bool{
		"#!/bin/sh\n# shellcheck shell=dash\n":  true,
		"#!/bin/sh\n# shellcheck disable=all\n": false,
		"echo\n# shellcheck shell=dash\n":       false,
	} {
		if got := HasShellDirective(text); got != want {
			t.Errorf("HasShellDirective(%q) = %t, want %t", text, got, want)
		}
	}
}
