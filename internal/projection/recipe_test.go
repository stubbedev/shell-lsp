package projection

import (
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/parser"
)

func recipeLines(host string) []RecipeLine {
	var out []RecipeLine
	start := 0
	for line := range strings.Lines(host) {
		end := start + len(strings.TrimSuffix(line, "\n"))
		command := start + len(line) - len(strings.TrimLeft(line, "\t"))
		out = append(out, RecipeLine{Start: start, Command: min(command, end), End: end, Comment: strings.HasPrefix(host[min(command, end):end], "#")})
		start += len(line)
	}
	return out
}

func copyLine(b *Builder) func(RecipeLine) {
	return func(l RecipeLine) { b.Verbatim(l.Command, l.End) }
}

func scriptTexts(m *Map) []string {
	var out []string
	for _, s := range m.Scripts() {
		out = append(out, m.Text()[s.Start:s.End])
	}
	return out
}

func TestLineShells(t *testing.T) {
	t.Parallel()

	host := "\t# keep\n\ta\n\t\n\tb\n"
	b := NewBuilder(host)
	b.Region(upper)
	shells := b.LineShells("set -e\n")
	for _, l := range recipeLines(host) {
		shells.Add(l, copyLine(b))
	}
	m := b.Map()
	want := []string{"# keep\n(\nset -e\na\n)\n", "(\nset -e\nb\n)\n"}
	if got := scriptTexts(m); !slices.Equal(got, want) {
		t.Errorf("scripts %q, want %q", got, want)
	}
}

func TestOneShell(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, host string
		want       []string
	}{
		{"one subshell around every line", "\t# keep\n\ta\n\t\n\tb\n", []string{"(\nset -e\n# keep\na\n\nb\n)\n"}},
		{"only comments open no subshell", "\t# one\n\t# two\n", []string{"# one\n# two\n"}},
	} {
		b := NewBuilder(tt.host)
		b.Region(upper)
		b.OneShell("set -e\n", recipeLines(tt.host), copyLine(b))
		m := b.Map()
		if got := scriptTexts(m); !slices.Equal(got, tt.want) {
			t.Errorf("%s: scripts %q, want %q", tt.name, got, tt.want)
		}
		if got := m.Scripts(); len(got) != 1 || got[0] != (parser.Span{Start: 0, End: len(m.Text())}) {
			t.Errorf("%s: script spans %v", tt.name, got)
		}
	}
}
