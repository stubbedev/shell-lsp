package lint

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func FuzzCheck(f *testing.F) {
	for _, seed := range []string{
		"echo $foo\nrm -rf $dir/*\n", "case $x in a) ;; a) ;; esac\n", "[ -v x ] && echo ${a:-${b}}\n", "cat <(<(x))\n",
		"echo {a,b}{1..3}\n", "f() { local x=$(y); }\n", "#!/bin/sh\necho ${x//a/b} $((1/0))\n", "echo ${${${\n",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		if len(src) > 4<<10 {
			return
		}
		for _, d := range []dialect.Dialect{dialect.Bash, dialect.Zsh, dialect.Posix, dialect.Dash, dialect.Ksh, dialect.Busybox} {
			findings := Check(parser.Parse(src, d), nil, Options{Optional: []string{allCodes}})
			fixSets := make([][]Fix, 0, len(findings))
			for _, finding := range findings {
				if finding.Span.Start < 0 || finding.Span.Start > finding.Span.End || finding.Span.End > len(src) {
					t.Fatalf("%s: %v span %+v out of bounds for %d bytes", d, finding.Code, finding.Span, len(src))
				}
				for _, fix := range finding.Fixes {
					for _, e := range fix.Edits {
						if e.Start < 0 || e.Start > e.End || e.End > len(src) {
							t.Fatalf("%s: %v fix %q edit %+v out of bounds for %d bytes", d, finding.Code, fix.Title, e, len(src))
						}
					}
				}
				fixSets = append(fixSets, finding.Fixes)
			}
			textedit.Apply(src, SafeEdits(fixSets))
		}
	})
}
