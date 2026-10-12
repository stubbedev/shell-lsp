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

func TestSourceSymbolsNameTheSourcedFile(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	render(Symbols(parser.Parse("source -- ./lib.sh\n. ./b.sh\nsource\n", dialect.Bash)), 0, &sb)
	if want := "./lib.sh [source] kind=0\n./b.sh [.] kind=0\n"; sb.String() != want {
		t.Errorf("symbols:\n%s\nwant:\n%s", sb.String(), want)
	}
}

const batsScript = `#!/usr/bin/env bats
setup_file() {
	export SHARED=1
}
setup() {
	load helper
}
teardown() { :; }
teardown_file() { :; }
@test "adds numbers" {
	local sum=3
	run add 1 2
}
helper() { :; }
@test "uses $SHARED" {
	[ "$SHARED" = 1 ]
}
`

func TestBatsSymbols(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	render(Symbols(parser.Parse(batsScript, dialect.Bash)), 0, &sb)
	want := `setup_file [bats hook, runs once before the file's tests] kind=1
  SHARED [exported variable] kind=2
setup [bats hook, runs before each test] kind=1
teardown [bats hook, runs after each test] kind=1
teardown_file [bats hook, runs once after the file's tests] kind=1
adds numbers [test] kind=6
  sum [local variable] kind=2
helper [function] kind=1
"uses $SHARED" [test] kind=6
`
	if sb.String() != want {
		t.Errorf("symbols:\n%s\nwant:\n%s", sb.String(), want)
	}

	var plain strings.Builder
	render(Symbols(parser.Parse("#!/bin/bash\nsetup() { :; }\n", dialect.Bash)), 0, &plain)
	if plain.String() != "setup [function] kind=1\n" {
		t.Errorf("setup outside a Bats file = %q, want a plain function", plain.String())
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
	for _, src := range []string{script, batsScript} {
		check(parser.Span{Start: 0, End: len(src)}, Symbols(parser.Parse(src, dialect.Bash)))
	}
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

func TestZshAlternateFormsFold(t *testing.T) {
	t.Parallel()

	src := "if [[ -n $a ]] {\n\tprint a\n} elif [[ -n $b ]] {\n\tprint b\n} else {\n\tprint c\n}\n" +
		"repeat 3 do\n\tprint r\ndone\n" +
		"foreach f (x y)\n\tprint $f\nend\n" +
		"while (( i < 3 )) {\n\t(( i++ ))\n}\n"
	lineOf := func(offset int) int { return strings.Count(src[:offset], "\n") }
	var got []string
	for _, f := range Folds(parser.Parse(src, dialect.Zsh), lineOf) {
		got = append(got, fmt.Sprintf("%d-%d:%d", f.StartLine, f.EndLine, f.Kind))
	}
	want := []string{"0-1:0", "2-3:0", "4-5:0", "7-8:0", "10-11:0", "13-14:0"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("folds = %v\nwant    %v", got, want)
	}
}
