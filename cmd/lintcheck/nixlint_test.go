package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host/nix"
)

func TestSamplingIsDeterministicAndSorted(t *testing.T) {
	t.Parallel()

	var files []nixLintFile
	for _, c := range "abcdefghij" {
		files = append(files, nixLintFile{path: "/r/" + string(c), rel: string(c)})
	}
	first := sampleNixFiles(files, 4)
	slices.Reverse(files)
	second := sampleNixFiles(files, 4)
	if !slices.Equal(first, second) || len(first) != 4 {
		t.Errorf("samples differ: %v, %v", first, second)
	}
	if !slices.IsSortedFunc(first, func(a, b nixLintFile) int { return strings.Compare(a.rel, b.rel) }) {
		t.Errorf("sample is not sorted: %v", first)
	}
	if got := sampleNixFiles(files, 100); len(got) != len(files) {
		t.Errorf("a large sample has %d files", len(got))
	}
}

func TestDeadnixMessagesPickARow(t *testing.T) {
	t.Parallel()

	for message, want := range map[string]string{
		"Unused let binding: a":     rowUnusedLet,
		"Unused lambda argument: a": rowUnusedArgument,
		"Unused lambda pattern: a":  rowUnusedPattern,
	} {
		if got := deadnixRow(message); got != want {
			t.Errorf("%s is %q, want %q", message, got, want)
		}
	}
}

func TestOurFindingsAreKeyedLikeTheirs(t *testing.T) {
	t.Parallel()

	const text = "let\n  /* é */ a = 1;\n  _a = 1;\nin\nx@{ y }: [ (y) ]\n"
	got := ourNixLints(text, nix.Analyze(text), nil)
	for _, want := range []nixLintKey{
		{place: nixPlace{2, 11}, row: rowUnusedLet},
		{place: nixPlace{3, 3}, row: rowUnusedLet},
		{place: nixPlace{5, 1}, row: rowUnusedPattern},
		{place: nixPlace{5, 12}, row: "useless_parens"},
	} {
		if !got[want] {
			t.Errorf("%+v is missing from %+v", want, got)
		}
	}
}

func TestBrokenFixesAreCaught(t *testing.T) {
	t.Parallel()

	const good = "x: y: { a = (x); b = y.b; c = !(x == y); d = [ ] ++ x; }\n"
	if got := brokenNixFixes(good, nix.Analyze(good)); len(got) != 0 {
		t.Errorf("good fixes reported: %q", got)
	}
}

func TestRenderingKeepsTheCounts(t *testing.T) {
	t.Parallel()

	r := nixLintResult{files: 2, rows: map[string]*nixLintRow{"useless_parens": {agree: 1, ours: 2, theirs: 3}}, differences: []nixLintDifference{{path: "a.nix", place: nixPlace{1, 2}, row: "useless_parens", side: "ours"}}}
	got := renderNixLint(r, -1)
	for _, want := range []string{"2 files compared", tableRow("useless_parens", "1", "2", "3"), "a.nix:1:2 useless_parens ours"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q lacks %q", got, want)
		}
	}
}
