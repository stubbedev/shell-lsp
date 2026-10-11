package lint

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func findingTexts(src string, d dialect.Dialect) []string {
	var out []string
	for _, f := range Check(parser.Parse(src, d), nil, Options{}) {
		out = append(out, f.Code.String()+" "+src[f.Span.Start:f.Span.End])
	}
	return out
}

func TestKsh93CompoundAssignmentsAreNotCommandsOrVariables(t *testing.T) {
	t.Parallel()

	src := "#!/bin/ksh\npoint=(x=1 y=(z=2))\nprint -r -- \"${point.x} ${point.y.z}\"\n" +
		"typeset -T Pt_t=(integer len=0; get() { :; })\nPt_t p\nprint -r -- \"${p.len}\"\n"
	if got := findingTexts(src, dialect.Ksh); len(got) != 0 {
		t.Errorf("findings = %q, want none for compound members and type definitions", got)
	}
}

func TestKsh93MathFunctionsAreCalledFromArithmetic(t *testing.T) {
	t.Parallel()

	src := "#!/bin/ksh\nfunction .sh.math.hypot x y {\n  .sh.value=$(( sqrt(x*x + y*y) ))\n}\nprint $(( hypot(3, 4) ))\n"
	if got := findingTexts(src, dialect.Ksh); len(got) != 0 {
		t.Errorf("findings = %q, want the math function counted as called", got)
	}
}

func TestZshAssignAlwaysExpansionAssigns(t *testing.T) {
	t.Parallel()

	src := "#!/bin/zsh\nprint -r -- \"${x::=1}\"\nprint -r -- \"$x\"\n"
	if got := findingTexts(src, dialect.Zsh); len(got) != 0 {
		t.Errorf("findings = %q, want ${x::=1} to assign x", got)
	}
}

func TestKsh93NamesWithDotsAreNotMisread(t *testing.T) {
	t.Parallel()

	src := "#!/bin/ksh\nnamespace shop {\n  function add { print -r -- \"$1\"; }\n}\n.shop.add apples\n" +
		"counter=0\nfunction counter.get { .sh.value=1; }\nprint -r -- \"$counter ${.sh.version}\"\n" +
		"typeset -T Stack_t=(integer size=0; push() { (( _.size++ )); })\nStack_t stack\nstack.push\n" +
		"case $1 in\n  a) print -r -- a ;;&\n  *) print -r -- any ;;\nesac\n"
	if got := findingTexts(src, dialect.Ksh); len(got) != 0 {
		t.Errorf("findings = %q, want none for name spaces, disciplines, type methods and ;;&", got)
	}
}
