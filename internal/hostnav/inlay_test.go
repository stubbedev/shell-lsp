package hostnav

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/parser"
)

func (tr *tree) hints(name string) []string {
	g := tr.group(name)
	text := g.Self.Doc.Source().Text()
	var out []string
	for _, h := range g.InlayHints(parser.Span{End: len(text)}) {
		out = append(out, fmt.Sprintf("%q %s", text[h.Offset:min(h.Offset+3, len(text))], h.Label))
	}
	return out
}

func TestJustfileInlayHints(t *testing.T) {
	t.Parallel()

	tr := newTree(t, map[string]string{
		"justfile": "version := \"1.\" + \"0\"\nx := replace(version, \".\", \"-\")\nbuild target profile=\"dev\":\n    echo {{target}} {{profile}}\nci: (build \"x\" profile)\n    just build debug\n",
	})
	want := []string{
		`"ver" s:`,
		`"\".\"" from:`,
		`"\"-\"" to:`,
		`", \"" = "1.0"`,
		`"\"x\"" target:`,
		`"deb" target:`,
	}
	if got := tr.hints("justfile"); !slices.Equal(got, want) {
		t.Errorf("hints =\n%q\nwant\n%q", got, want)
	}
}

func TestMakefileInlayHints(t *testing.T) {
	t.Parallel()

	tr := newTree(t, map[string]string{
		"Makefile": "CC := gcc\nobjs := $(patsubst %.c,%.o,$(srcs))\nall:\n\t$(CC) -o app $(objs)\n",
	})
	want := []string{
		`"%.c" pattern:`,
		`"%.o" replacement:`,
		`"$(s" text:`,
		`") -" = gcc`,
	}
	if got := tr.hints("Makefile"); !slices.Equal(got, want) {
		t.Errorf("hints =\n%q\nwant\n%q", got, want)
	}
}
