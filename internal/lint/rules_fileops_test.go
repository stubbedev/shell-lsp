package lint

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func TestDeprecateWhichIsOptional(t *testing.T) {
	t.Parallel()

	codes := func(src string, optional ...string) []Code {
		var out []Code
		for _, f := range Check(parser.Parse(src, dialect.Bash), nil, Options{Optional: optional}) {
			out = append(out, f.Code)
		}
		return out
	}
	if got := codes("which javac\n", "deprecate-which"); !slices.Equal(got, []Code{2230}) {
		t.Errorf("enabled which javac = %v, want [SC2230]", got)
	}
	if got := codes("command -v javac\n", "deprecate-which"); len(got) != 0 {
		t.Errorf("enabled command -v javac = %v, want none", got)
	}
	if got := codes("which javac\n"); len(got) != 0 {
		t.Errorf("disabled which javac = %v, want none", got)
	}
}

func TestNormalizePath(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"//usr///":  "/usr",
		"/":         "/",
		"///":       "/",
		"/etc/**/*": "/etc/*/*",
		"/home//*":  "/home/*",
	} {
		if got := normalizePath(in); got != want {
			t.Errorf("normalizePath(%q) = %q, want %q", in, got, want)
		}
	}
}
