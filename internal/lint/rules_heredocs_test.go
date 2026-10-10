package lint

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func TestHeredocEndTokens(t *testing.T) {
	t.Parallel()

	for script, want := range map[string][]string{
		"cat <<EOF\nhello\nEOF \nEOF\n":           {"SC1118 3:4"},
		"cat <<-EOF\n\thello\n\tEOF # x\n\tEOF\n": {"SC1120 3:6"},
		"cat <<EOF\n\tEOF;\nEOF\n":                nil,
	} {
		tree := parser.Parse(script, dialect.Bash)
		var got []string
		for _, f := range Check(tree, nil, Options{}) {
			got = append(got, f.Code.String()+" "+position(script, f.Span.Start))
		}
		if !slices.Equal(got, want) {
			t.Errorf("%q: got %v, want %v", script, got, want)
		}
	}
}
