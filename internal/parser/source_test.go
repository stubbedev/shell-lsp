package parser

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

func TestSourcedFile(t *testing.T) {
	t.Parallel()

	for src, want := range map[string]string{
		"source lib.sh":            "lib.sh",
		". ./lib.sh extra":         "./lib.sh",
		"source -- lib.sh":         "lib.sh",
		"source -p \"$P\" lib.sh":  "lib.sh",
		"source":                   "",
		"source -p dir":            "",
		"echo lib.sh":              "",
		"builtin source -- lib.sh": "",
	} {
		tree := Parse(src+"\n", dialect.Bash)
		got := ""
		if word, ok := tree.Commands[0].SourcedFile(); ok {
			got = src[Offset(word.Pos()):Offset(word.End())]
		}
		if got != want {
			t.Errorf("SourcedFile(%q) = %q, want %q", src, got, want)
		}
	}
}
