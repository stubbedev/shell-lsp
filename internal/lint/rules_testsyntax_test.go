package lint

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func TestTestCommandSyntax(t *testing.T) {
	t.Parallel()

	for script, want := range map[string][]string{
		"[ a ] b\n":                        {"SC1140 1:7"},
		"[ x = - ]\n":                      nil,
		"[ x = foo= ]\n":                   nil,
		"[ x \\>= y ]\n":                   nil,
		"[ !-e /tmp/ready ]\n":             nil,
		"test 3 + 1 -gt 2\n":               {"SC1076 1:8"},
		"[ a= b ]\n":                       {"SC1108 1:3"},
		"if [ -f a ]; then :; else:; fi\n": {"SC1130 1:26"},
	} {
		tree := parser.Parse(script, dialect.Bash)
		var got []string
		for _, f := range Check(tree, nil, Options{}) {
			_, testSyntax := testSyntaxFamily.codes[f.Code]
			_, typo := typosFamily.codes[f.Code]
			if testSyntax || typo {
				got = append(got, f.Code.String()+" "+position(script, f.Span.Start))
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("%q: got %v, want %v", script, got, want)
		}
	}
}
