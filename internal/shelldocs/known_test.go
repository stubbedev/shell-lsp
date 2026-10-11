package shelldocs

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

func TestDocumentedVariablesAreKnownParameters(t *testing.T) {
	t.Parallel()

	for _, d := range []dialect.Dialect{dialect.Bash, dialect.Zsh, dialect.Posix, dialect.Dash, dialect.Ksh, dialect.Busybox} {
		known := KnownParameters(d)
		for _, e := range Variables(d) {
			if !known.Has(e.Name) {
				t.Errorf("%s documents %s, but it is missing from its known parameters", d, e.Name)
			}
		}
	}
}

func TestKnownParametersFollowTheDialect(t *testing.T) {
	t.Parallel()

	if !KnownParameters(dialect.Zsh).Is("commands", AssociativeParameter) || KnownParameters(dialect.Bash).Has("commands") {
		t.Error("commands must be a zsh associative array and unknown to bash")
	}
	if !KnownParameters(dialect.Zsh).Has("vcs_info_msg_0_") || KnownParameters(dialect.Zsh).Has("vcs_info_msg_x_") {
		t.Error("numbered vcs_info messages must match digits only")
	}
	if !KnownParameters(dialect.Bash).Is("PS1", PromptParameter) {
		t.Error("PS1 must be a prompt")
	}
}
