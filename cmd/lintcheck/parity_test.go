package main

import (
	"maps"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/lint"
)

func sampleParity() parity {
	tallies := map[lint.Code]tally{}
	for i, code := range implementedCodes() {
		tallies[code] = tally{agree: i, oursOnly: i % 3, theirsOnly: i % 5}
	}
	return parity{compared: 42, skipped: 2, crashed: []string{"a/b.sh", "c/d.sh"}, tallies: tallies}
}

func TestParityBaselineRoundTrips(t *testing.T) {
	t.Parallel()

	want := sampleParity()
	text := renderParity(want)
	got, err := parseParity(text)
	if err != nil {
		t.Fatal(err)
	}
	if problems := diffParity(want, got); len(problems) != 0 {
		t.Errorf("a rendered baseline reads back differently: %v", problems)
	}
	if renderParity(got) != text {
		t.Error("rendering the baseline again changes it")
	}
}

func TestDiffParityTellsRegressionsFromImprovements(t *testing.T) {
	t.Parallel()

	codes := implementedCodes()
	worse, better := codes[0], codes[1]
	want := sampleParity()
	got := sampleParity()
	got.tallies = maps.Clone(want.tallies)
	w := got.tallies[worse]
	w.theirsOnly++
	got.tallies[worse] = w
	b := got.tallies[better]
	b.agree++
	got.tallies[better] = b
	problems := diffParity(want, got)
	if len(problems) != 2 {
		t.Fatalf("problems = %+v, want one per changed code", problems)
	}
	if problems[0].code != worse || !problems[0].regressed || problems[1].code != better || problems[1].regressed {
		t.Errorf("problems = %+v, want %s regressed and %s improved", problems, worse, better)
	}
	got = sampleParity()
	got.crashed = got.crashed[:1]
	if problems := diffParity(want, got); len(problems) != 1 || problems[0].regressed {
		t.Errorf("a change in the scripts ShellCheck crashes on gave %+v", problems)
	}
}

func TestParseParityNeedsTheSummary(t *testing.T) {
	t.Parallel()

	if _, err := parseParity("| SC2086 | 1 | 2 | 3 |\n"); err == nil {
		t.Error("a baseline without its summary line parsed")
	}
}
