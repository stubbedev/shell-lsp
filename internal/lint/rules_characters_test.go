package lint

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func TestCharacterChecks(t *testing.T) {
	t.Parallel()

	for script, want := range map[string][]string{
		"bazel test \\\n  //a:b \\\n  # //c:d \\\n\necho done\n": {"SC1143 3:12", "SC1010 5:6"},
		"echo {a,{b,c}} {$x,$y} @{u}\n":                          {"SC1083 1:25", "SC1083 1:27"},
		"source \"$dir\"/lib.sh\n":                               {"SC1091 1:8"},
		"cat <<EOF\n“quoted” $hosts[1]\nEOF\n":                   {"SC1111 2:1", "SC1111 2:8", "SC1087 2:10"},
		"cat <<'EOF'\n“quoted”\nEOF\n":                           nil,
	} {
		tree := parser.Parse(script, dialect.Bash)
		var got []string
		for _, f := range Check(tree, nil, Options{Exclude: []Code{2086, 2154}}) {
			got = append(got, f.Code.String()+" "+position(script, f.Span.Start))
		}
		if !slices.Equal(got, want) {
			t.Errorf("%q: got %v, want %v", script, got, want)
		}
	}
}
