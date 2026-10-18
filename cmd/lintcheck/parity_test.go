package main

import (
	"maps"
	"strings"
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

func TestParseParityReadsOnlyItsOwnLines(t *testing.T) {
	t.Parallel()

	const summary = "7 scripts compared, 1 skipped as not UTF-8 or with CRLF line endings.\n"
	p, err := parseParity("# title\n" + summary + "prose with | a bar and 3 scripts compared\n- `a b.sh`\n" + parityHeader + "\n" + parityDelimiter + "\n| SC2086 | 1 | 02 | 3 |\n")
	if err != nil {
		t.Fatal(err)
	}
	code, err := lint.ParseCode("SC2086")
	if err != nil {
		t.Fatal(err)
	}
	if p.compared != 7 || p.skipped != 1 || len(p.crashed) != 1 || p.crashed[0] != "a b.sh" || len(p.tallies) != 1 || p.tallies[code] != (tally{agree: 1, oursOnly: 2, theirsOnly: 3}) {
		t.Fatalf("parity = %+v", p)
	}
	for _, line := range []string{
		"7 scripts compared, 1 skipped as not UTF-8 or with CRLF line endings",
		"7 scripts compared, 1 skipped as not UTF-8 or with CRLF line endings. ",
		"x7 scripts compared, 1 skipped as not UTF-8 or with CRLF line endings.",
		"7 scripts compared, -1 skipped as not UTF-8 or with CRLF line endings.",
		"99999999999999999999 scripts compared, 1 skipped as not UTF-8 or with CRLF line endings.",
	} {
		if _, _, ok := parseSummary(line); ok {
			t.Errorf("parseSummary(%q) took it for the summary", line)
		}
	}
	for _, listing := range []string{
		"- `a.sh",
		"- ``",
		"- `a`b`",
		"| SC2086 | 1 | 2 |",
		"| SC2086 | 1 | 2 | 3 | 4 |",
		"| SC2086 | 1 | 2 | x |",
		"| SC2086 | 1 | 2 | -3 |",
		"| sc2086 | 1 | 2 | 3 |",
		"| SC02086 | 1 | 2 | 3 |",
		"| SC2086 | 1 | 2 | 3 |  ",
		"|SC2086 | 1 | 2 | 3 |",
		"| SC2086 | 1 | 2 | 99999999999999999999 |",
		"| SC2086 | 1 | 2 | 3 |\n| SC2086 | 1 | 2 | 3 |",
	} {
		if _, err := parseParity(summary + listing + "\n"); err == nil {
			t.Errorf("parseParity accepted %q", listing)
		} else if !strings.HasPrefix(err.Error(), "line ") {
			t.Errorf("parseParity(%q) = %v, want the line named", listing, err)
		}
	}
}
