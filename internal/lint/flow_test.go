package lint

import (
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

func lostReads(t *testing.T, src string, d dialect.Dialect) []string {
	t.Helper()
	tree := parser.Parse(src, d)
	result := analyzeFlow(tree, nil, tree.Bats())
	var out []string
	parser.Walk(tree.File, func(n syntax.Node) bool {
		if _, ok := result.lostRead(n); ok {
			span, _ := tree.NodeSpan(n)
			out = append(out, src[span.Start:span.End])
		}
		return true
	})
	return out
}

func TestFlowTracksWritesLostInSubshells(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		src  string
		d    dialect.Dialect
		want []string
	}{
		{"pipeline", "cmd | while read -r x; do :; done\necho \"$x\"\n", dialect.Bash, []string{"$x"}},
		{"later write revives", "( x=1 )\nx=2\necho \"$x\"\n", dialect.Bash, nil},
		{"one branch revives", "( x=1 )\nif c; then x=2; fi\necho \"$x\"\n", dialect.Bash, []string{"$x"}},
		{"arithmetic read", "( n=1 )\n(( n++ ))\n", dialect.Bash, []string{"n"}},
		{"bash lastpipe", "shopt -s lastpipe\necho a | read -r x\necho \"$x\"\n", dialect.Bash, nil},
		{"ksh last stage", "echo a | read -r x\necho \"$x\"\n", dialect.Ksh, nil},
		{"dash first stage", "x=1 | cat\necho \"$x\"\n", dialect.Dash, []string{"$x"}},
		{"process substitution", "cat <(x=1)\necho \"$x\"\n", dialect.Bash, nil},
		{"non-forking substitution", "y=${ x=1; echo; }\necho \"$x\"\n", dialect.Bash, nil},
		{"through function summary", "f() { cmd | while read -r x; do :; done; }\nf\necho \"$x\"\n", dialect.Bash, []string{"$x"}},
		{"function writes nothing in caller subshell", "f() { x=1; }\n( f )\necho \"$x\"\n", dialect.Bash, nil},
		{"local stays local", "f() { local x; cmd | read -r x; }\nf\necho \"$x\"\n", dialect.Bash, nil},
		{"declaration without value", "( export X )\necho \"$X\"\n", dialect.Bash, nil},
		{"IFS ignored", "( IFS=: )\necho \"$IFS\"\n", dialect.Bash, nil},
	}
	for _, c := range cases {
		if got := lostReads(t, c.src, c.d); !slices.Equal(got, c.want) {
			t.Errorf("%s: lost reads = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestFlowReportsLatestLostWrite(t *testing.T) {
	t.Parallel()

	src := "( x=1 )\n( x=2 )\necho \"$x\"\n"
	tree := parser.Parse(src, dialect.Bash)
	result := analyzeFlow(tree, nil, false)
	var got []lostWrite
	for _, w := range result.lostReads {
		got = append(got, w)
	}
	if len(got) != 1 || src[got[0].site.Start:got[0].site.End] != "x=2" || got[0].kind != subshellGroup {
		t.Errorf("lost writes = %+v, want the second group assignment", got)
	}
}

func TestEverySubshellKindHasALabel(t *testing.T) {
	t.Parallel()

	for k := notSubshell + 1; k <= subshellScript; k++ {
		if k.label() == "" {
			t.Errorf("subshell kind %d has an empty label", k)
		}
	}
}

func TestSubshellMessagesNameTheSameKinds(t *testing.T) {
	t.Parallel()

	lost := findingsWith(t, "x=1\n: `x=2`\necho \"$x\"\n", 2030)
	if len(lost) != 1 || !strings.Contains(lost[0].Message, "backtick expansion") {
		t.Errorf("SC2030 for a backtick expansion = %+v", lost)
	}
	jump := findingsWith(t, "for i in 1 2; do\n  (break)\ndone\n", 2106)
	if len(jump) != 1 || !strings.Contains(jump[0].Message, "this ( ) group") {
		t.Errorf("SC2106 in a ( ) group = %+v", jump)
	}
}
