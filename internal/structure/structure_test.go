package structure

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

const script = `#!/bin/bash
# Header comment
# continues here
source ./a.sh
. ./b.sh
declare -A map
readonly VERSION=1
count=0
count=1
deploy() {
	local -a targets=(a b)
	inner() {
		local depth=1
	}
	GLOBAL_FROM_FN=1
	cat <<END
body
END
}
# region setup
if true; then
	echo yes
else
	echo no
fi
for f in *; do
	case $f in
		*.sh)
			echo sh
			;;
	esac
done
arr=(
	one
	two
)
# endregion
`

func render(symbols []Symbol, depth int, sb *strings.Builder) {
	for _, s := range symbols {
		fmt.Fprintf(sb, "%s%s [%s] kind=%d\n", strings.Repeat("  ", depth), s.Name, s.Detail, s.Kind)
		render(s.Children, depth+1, sb)
	}
}

func TestSymbols(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	render(Symbols(parser.Parse(script, dialect.Bash)), 0, &sb)
	want := `./a.sh [source] kind=0
./b.sh [.] kind=0
map [associative array] kind=3
VERSION [read-only] kind=4
count [variable] kind=2
deploy [function] kind=1
  targets [local array] kind=3
  inner [function] kind=1
    depth [local variable] kind=2
  GLOBAL_FROM_FN [variable] kind=2
  <<END [heredoc] kind=5
f [variable] kind=2
arr [array] kind=3
`
	if sb.String() != want {
		t.Errorf("symbols:\n%s\nwant:\n%s", sb.String(), want)
	}
}

func TestSymbolSpansNestInParents(t *testing.T) {
	t.Parallel()

	var check func(parent parser.Span, symbols []Symbol)
	check = func(parent parser.Span, symbols []Symbol) {
		for _, s := range symbols {
			if s.Span.Start < parent.Start || s.Span.End > parent.End {
				t.Errorf("%s span %+v escapes parent %+v", s.Name, s.Span, parent)
			}
			if s.Selection.Start < s.Span.Start || s.Selection.End > s.Span.End {
				t.Errorf("%s selection %+v outside span %+v", s.Name, s.Selection, s.Span)
			}
			check(s.Span, s.Children)
		}
	}
	check(parser.Span{Start: 0, End: len(script)}, Symbols(parser.Parse(script, dialect.Bash)))
}

func TestFolds(t *testing.T) {
	t.Parallel()

	lineOf := func(offset int) int { return strings.Count(script[:offset], "\n") }
	var got []string
	for _, f := range Folds(parser.Parse(script, dialect.Bash), lineOf) {
		got = append(got, fmt.Sprintf("%d-%d:%d", f.StartLine, f.EndLine, f.Kind))
	}
	want := []string{
		"1-2:2",
		"3-4:3",
		"9-17:0",
		"11-12:0",
		"15-17:0",
		"19-36:1",
		"20-23:0",
		"22-23:0",
		"25-30:0",
		"26-29:0",
		"27-29:0",
		"32-34:0",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("folds = %v\nwant    %v", got, want)
	}
}

func TestLinkedDelimiters(t *testing.T) {
	t.Parallel()

	text := "cat <<EOF\nbody\nEOF\ncat <<-'END'\n\tx\n\tEND\n"
	tree := parser.Parse(text, dialect.Bash)
	for _, at := range []int{strings.Index(text, "EOF"), strings.LastIndex(text, "EOF") + 1} {
		spans, ok := LinkedDelimiters(tree, at)
		if !ok || len(spans) != 2 || text[spans[0].Start:spans[0].End] != "EOF" || text[spans[1].Start:spans[1].End] != "EOF" || spans[0] == spans[1] {
			t.Errorf("at %d: spans = %+v, %t", at, spans, ok)
		}
	}
	quoted, ok := LinkedDelimiters(tree, strings.Index(text, "END"))
	if !ok || text[quoted[0].Start:quoted[0].End] != "END" || text[quoted[1].Start:quoted[1].End] != "END" {
		t.Errorf("quoted dash heredoc spans = %+v, %t", quoted, ok)
	}
	if _, ok := LinkedDelimiters(tree, strings.Index(text, "body")); ok {
		t.Error("linked editing offered inside a heredoc body")
	}
}
