package lint

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func TestShebangLines(t *testing.T) {
	t.Parallel()

	for script, want := range map[string][]string{
		"# Header\n# ! Note\necho hi\n":     {"SC1115 2:2", "SC1128 2:1"},
		"\n  #!/bin/sh\necho hi\n":          {"SC1114 2:1", "SC1128 2:1"},
		"\n! /bin/sh\necho hi\n":            {"SC1104 2:1", "SC1128 2:1"},
		"  #/bin/sh\necho hi\n":             nil,
		"#!/bin/sh\n\n#!/bin/sh\necho hi\n": nil,
		"# /etc/foo config\necho hi\n":      {"SC1113 1:2"},
		"#  !  /bin/sh\necho hi\n":          {"SC1115 1:2"},
		"!/bin/sh\necho hi\n":               {"SC1104 1:1"},
		"echo hi\n#!/bin/sh\n":              nil,
	} {
		tree := parser.Parse(script, dialect.Posix)
		var got []string
		for _, f := range Check(tree, nil, Options{Exclude: []Code{2239}}) {
			got = append(got, f.Code.String()+" "+position(script, f.Span.Start))
		}
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("%q: got %v, want %v", script, got, want)
		}
	}
}
