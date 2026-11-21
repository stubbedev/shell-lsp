package shelldocs

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

func TestDocumentedVariablesAreKnownParameters(t *testing.T) {
	t.Parallel()

	for s := range dialect.All.Shells().Members() {
		known := KnownParameters(s)
		for _, e := range Variables(s) {
			if !known.Has(e.Name) {
				t.Errorf("%s documents %s, but it is missing from its known parameters", s.Lang(), e.Name)
			}
		}
	}
}

func TestKnownParametersFollowTheDialect(t *testing.T) {
	t.Parallel()

	if !KnownParameters(dialect.Zsh.Shell()).Is("commands", AssociativeParameter) || KnownParameters(dialect.Bash.Shell()).Has("commands") {
		t.Error("commands must be a zsh associative array and unknown to bash")
	}
	if !KnownParameters(dialect.Zsh.Shell()).Has("vcs_info_msg_0_") || KnownParameters(dialect.Zsh.Shell()).Has("vcs_info_msg_x_") {
		t.Error("numbered vcs_info messages must match digits only")
	}
	if !KnownParameters(dialect.Bash.Shell()).Is("PS1", PromptParameter) {
		t.Error("PS1 must be a prompt")
	}
}
