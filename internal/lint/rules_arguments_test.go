package lint

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func fixed(t *testing.T, src string, code Code) string {
	t.Helper()
	for _, f := range Check(parser.Parse(src, dialect.Bash), nil, Options{}) {
		if f.Code == code {
			return textedit.Apply(src, fixEdits(f))
		}
	}
	t.Fatalf("no %s finding in %q", code, src)
	return ""
}

func TestRecursiveCopyFixKeepsOtherFlags(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"cp -r src/ dst/\n":       "cp -R src/ dst/\n",
		"cp -vrp src/ dst/\n":     "cp -vRp src/ dst/\n",
		"command cp -ri a b\n":    "command cp -Ri a b\n",
		"cp -v -r -- src/ dst/\n": "cp -v -R -- src/ dst/\n",
	}
	for src, want := range tests {
		if got := fixed(t, src, 2336); got != want {
			t.Errorf("fix of %q = %q, want %q", src, got, want)
		}
	}
}
