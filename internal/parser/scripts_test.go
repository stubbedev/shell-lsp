package parser

import (
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

func TestScriptsParseIndependently(t *testing.T) {
	t.Parallel()

	first := "echo \"unclosed\n"
	second := "# shellcheck disable=SC2086\nfor x in $y; do\n  echo $x\ndone\n"
	third := "if true; then\n"
	text := first + second + third
	scripts := []Span{
		{Start: 0, End: len(first)},
		{Start: len(first), End: len(first) + len(second)},
		{Start: len(first) + len(second), End: len(text)},
	}
	tree := ParseScripts(nil, text, scripts, dialect.Bash, dialect.Bash.Language(), nil)
	if tree.Synthetic(0) {
		t.Error("a tree without an origin has synthetic text")
	}

	var lines []int
	for _, e := range tree.Errors {
		lines = append(lines, tree.Line(e.Span.Start))
	}
	if !slices.Equal(lines, []int{1, 6, 6}) {
		t.Errorf("errors on lines %v (%+v), want errors only in the first and the third script", lines, tree.Errors)
	}
	echo := strings.Index(text, "echo $x")
	i := slices.IndexFunc(tree.Commands, func(c Command) bool { return c.Name == "echo" && c.Span.Start == echo })
	if i < 0 {
		t.Fatalf("the second script's echo is not at offset %d: %+v", echo, tree.Commands)
	}
	if got := tree.Line(tree.Commands[i].Span.Start); got != 4 {
		t.Errorf("the second script's echo is on line %d, want 4", got)
	}
	comment := strings.Index(text, "# shellcheck")
	if len(tree.Comments) != 1 || tree.Comments[0].Span.Start != comment {
		t.Errorf("comments = %+v, want the directive at %d", tree.Comments, comment)
	}
}

func TestScriptsReportTheirLinesInTheSource(t *testing.T) {
	t.Parallel()

	text := "(\necho a\n)\n"
	tree := ParseScripts(nil, text, []Span{{Start: 0, End: len(text)}}, dialect.Posix, dialect.Posix.Language(), offsetOrigin{})
	if got := tree.SourceLine(2); got != 42 {
		t.Errorf("SourceLine(2) = %d, want what the source numbering says", got)
	}
	if !tree.Synthetic(0) || tree.Synthetic(2) {
		t.Errorf("Synthetic(0) = %v, Synthetic(2) = %v, want what the origin says", tree.Synthetic(0), tree.Synthetic(2))
	}
	if got := tree.Line(2); got != 2 {
		t.Errorf("Line(2) = %d, want the line within the text", got)
	}
	if plain := Parse(text, dialect.Posix); plain.SourceLine(2) != plain.Line(2) {
		t.Errorf("a plain tree numbers source lines %d, text lines %d", plain.SourceLine(2), plain.Line(2))
	}
}

type offsetOrigin struct{}

func (offsetOrigin) SourceLine(offset int) int { return 40 + offset }

func (offsetOrigin) Synthetic(offset int) bool { return offset == 0 }
