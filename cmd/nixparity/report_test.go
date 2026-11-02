package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderingIsStable(t *testing.T) {
	t.Parallel()

	first := renderMarkdown(sampleReport())
	if second := renderMarkdown(sampleReport()); first != second {
		t.Errorf("two renderings differ:\n%s\n---\n%s", first, second)
	}
	a, err := encodeJSON(sampleReport())
	if err != nil {
		t.Fatal(err)
	}
	b, err := encodeJSON(sampleReport())
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Error("two JSON encodings differ")
	}
	if !json.Valid(a) || !strings.HasSuffix(string(a), "}\n") {
		t.Errorf("JSON report is not one newline-terminated value: %s", a)
	}
}

func TestMarkdownNamesEveryFeatureInOrderAndEveryFailure(t *testing.T) {
	t.Parallel()

	text := renderMarkdown(sampleReport())
	last := -1
	agreement := text[strings.Index(text, "## Agreement"):strings.Index(text, "## Answer items")]
	for _, f := range allFeatures() {
		at := strings.Index(agreement, "| "+f.String()+" |")
		if at <= last {
			t.Errorf("%s is missing or out of order in:\n%s", f, agreement)
		}
		last = at
	}
	for _, want := range []string{
		"| definition | 3 | 0 | 1 | 0 | 1 | 0 | 1 |",
		"| definition | 1 | 1 | 0 | 0 | 0 | 0 | 1 | 0 |",
		"| hover | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 1 |",
		"| shell-lsp | 3.0 MiB | not measured | 0 |",
		"| nixd 2 | not measured | not measured | 1 |",
		"- definition a.nix:1:3: failed",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the report lacks %q:\n%s", want, text)
		}
	}
}

func TestReportsCarryNoAnswerText(t *testing.T) {
	t.Parallel()

	r := sampleReport()
	raw, err := encodeJSON(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{`"items":["a"`, `"a", "b"`} {
		if strings.Contains(string(raw), leaked) {
			t.Errorf("the JSON report holds answer items: %s", raw)
		}
	}
}

func TestBaselineRoundTrips(t *testing.T) {
	t.Parallel()

	want := sampleReport().Counts
	raw, err := encodeJSON(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseBaseline(raw)
	if err != nil {
		t.Fatal(err)
	}
	if problems := diffCounts(want, got); len(problems) != 0 {
		t.Errorf("a written baseline reads back differently: %+v", problems)
	}
	again, err := encodeJSON(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(raw) {
		t.Error("writing the baseline again changes it")
	}
}

func TestParseBaselineRejectsWhatItDoesNotWrite(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		`{"unknown":1}`,
		`{"features":[{"feature":"nonsense"}]}`,
		`{} {}`,
		`not json`,
	} {
		if _, err := parseBaseline([]byte(raw)); err == nil {
			t.Errorf("parseBaseline accepted %s", raw)
		}
	}
}

func TestDiffCountsTellsRegressionsFromImprovements(t *testing.T) {
	t.Parallel()

	change := func(f func(*featureCounts)) counts {
		c := sampleReport().Counts
		f(&c.Features[featureDefinition])
		return c
	}
	tests := []struct {
		name      string
		edit      func(*featureCounts)
		regressed bool
	}{
		{"fewer agreements", func(c *featureCounts) { c.Agree-- }, true},
		{"more disagreements", func(c *featureCounts) { c.Disagree++ }, true},
		{"more missing from ours", func(c *featureCounts) { c.OursMissing++ }, true},
		{"more failures", func(c *featureCounts) { c.Failed++ }, true},
		{"more agreements", func(c *featureCounts) { c.Agree++ }, false},
		{"fewer missing from ours", func(c *featureCounts) { c.OursMissing-- }, false},
		{"more missing from theirs", func(c *featureCounts) { c.TheirsMissing++ }, false},
	}
	base := sampleReport().Counts
	for _, tt := range tests {
		problems := diffCounts(base, change(tt.edit))
		if len(problems) != 1 || problems[0].feature != featureDefinition || problems[0].regressed != tt.regressed {
			t.Errorf("%s: problems = %+v, want definition with regressed = %v", tt.name, problems, tt.regressed)
		}
	}
	moved := sampleReport().Counts
	moved.Scope.Theirs.Version = "3"
	if problems := diffCounts(base, moved); len(problems) != 1 || problems[0].regressed {
		t.Errorf("a new reference version gave %+v", problems)
	}
	skipped := sampleReport().Counts
	skipped.Skipped = []string{"big.nix"}
	if problems := diffCounts(base, skipped); len(problems) != 1 {
		t.Errorf("a new skipped file gave %+v", problems)
	}
}

func TestDiffCountsIgnoresPerformance(t *testing.T) {
	t.Parallel()

	a, b := sampleReport(), sampleReport()
	b.Ours.Latency[featureDefinition].P99Micros += 1000
	b.Theirs.Restarts += 5
	if problems := diffCounts(a.Counts, b.Counts); len(problems) != 0 {
		t.Errorf("a change in latency or restarts drifted: %+v", problems)
	}
}
