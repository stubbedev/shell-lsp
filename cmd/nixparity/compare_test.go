package main

import (
	"slices"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

func TestJudge(t *testing.T) {
	t.Parallel()

	answered := func(items ...string) answer { return answer{outcome: outcomeAnswered, items: items} }
	bare := func(o outcome) answer { return answer{outcome: o} }
	definition, hover := featureDefinition.spec(), featureHover.spec()
	tests := []struct {
		name         string
		spec         featureSpec
		ours, theirs answer
		want         verdict
	}{
		{"same items", definition, answered("a"), answered("a"), verdictAgree},
		{"different items", definition, answered("a"), answered("b"), verdictDisagree},
		{"both empty", definition, bare(outcomeEmpty), bare(outcomeEmpty), verdictBothEmpty},
		{"refused and empty", definition, bare(outcomeRefused), bare(outcomeEmpty), verdictBothEmpty},
		{"ours empty", definition, bare(outcomeEmpty), answered("a"), verdictOursMissing},
		{"ours unsupported", definition, bare(outcomeUnsupported), answered("a"), verdictOursMissing},
		{"ours silent", definition, bare(outcomeSilent), answered("a"), verdictOursMissing},
		{"theirs refused", definition, answered("a"), bare(outcomeRefused), verdictTheirsMissing},
		{"ours timed out, both otherwise silent", definition, bare(outcomeTimeout), bare(outcomeEmpty), verdictFailed},
		{"ours crashed against an answer", definition, bare(outcomeCrashed), answered("a"), verdictFailed},
		{"theirs crashed against an answer", definition, answered("a"), bare(outcomeCrashed), verdictFailed},
		{"both timed out", definition, bare(outcomeTimeout), bare(outcomeTimeout), verdictFailed},
		{"malformed", definition, bare(outcomeMalformed), answered("a"), verdictFailed},
		{"hover ranges differ", hover, answered("a"), answered("b"), verdictDisagree},
		{"hover without a range", hover, answered(), answered("b"), verdictAgree},
	}
	for _, tt := range tests {
		if got := judge(tt.spec, tt.ours, tt.theirs); got != tt.want {
			t.Errorf("%s: judge = %s, want %s", tt.name, got, tt.want)
		}
	}
}

func TestOverlap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		a, b []string
		want itemTally
	}{
		{nil, nil, itemTally{}},
		{[]string{"a", "b"}, []string{"a", "b"}, itemTally{Shared: 2}},
		{[]string{"a", "c"}, []string{"b", "c", "d"}, itemTally{Shared: 1, OursOnly: 1, TheirsOnly: 2}},
		{[]string{"a"}, nil, itemTally{OursOnly: 1}},
	}
	for _, tt := range tests {
		if got := overlap(tt.a, tt.b); got != tt.want {
			t.Errorf("overlap(%q, %q) = %+v, want %+v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestPercentile(t *testing.T) {
	t.Parallel()

	hundred := make([]time.Duration, 100)
	for i := range hundred {
		hundred[i] = time.Duration(i + 1)
	}
	tests := []struct {
		name    string
		samples []time.Duration
		p       int
		want    time.Duration
	}{
		{"nothing", nil, 50, 0},
		{"one sample", []time.Duration{7}, 99, 7},
		{"median of two is the lower", []time.Duration{1, 2}, 50, 1},
		{"p90 of two is the upper", []time.Duration{1, 2}, 90, 2},
		{"p50 of a hundred", hundred, 50, 50},
		{"p90 of a hundred", hundred, 90, 90},
		{"p99 of a hundred", hundred, 99, 99},
		{"p100 of a hundred", hundred, 100, 100},
	}
	for _, tt := range tests {
		if got := percentile(tt.samples, tt.p); got != tt.want {
			t.Errorf("%s: percentile = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func samplePlan() plan {
	doc := docstore.NewDocument("file:///w/a.nix", languageID, 1, "abc\n")
	return plan{
		root:     "/w",
		settings: settings{seed: 9, positions: 2, fileLimit: 5},
		docs:     []document{{rel: "a.nix", doc: doc}},
		queries: []query{
			{feature: featureDefinition, position: lsp.Position{Line: 0, Character: 1}},
			{feature: featureDefinition, position: lsp.Position{Line: 0, Character: 2}},
			{feature: featureDefinition, position: lsp.Position{Line: 0, Character: 3}},
			{feature: featureHover, position: lsp.Position{Line: 0, Character: 1}},
		},
	}
}

func sampleReport() report {
	peak := int64(3 << 20)
	ours := sideRun{identity: identity{Name: "shell-lsp"}, peakRSS: &peak, answers: []answer{
		{outcome: outcomeAnswered, items: []string{"a", "b"}, latency: time.Millisecond},
		{outcome: outcomeTimeout},
		{outcome: outcomeEmpty, latency: 2 * time.Millisecond},
		{outcome: outcomeCrashed},
	}}
	theirs := sideRun{identity: identity{Name: "nixd", Version: "2"}, restarts: 1, answers: []answer{
		{outcome: outcomeAnswered, items: []string{"b", "c"}, latency: 4 * time.Millisecond},
		{outcome: outcomeEmpty, latency: 5 * time.Millisecond},
		{outcome: outcomeAnswered, items: []string{"d"}, latency: 6 * time.Millisecond},
		{outcome: outcomeAnswered, latency: 7 * time.Millisecond},
	}}
	return compareRuns(samplePlan(), ours, theirs, 1)
}

func TestCompareRunsNeverCountsAFailureAsAnAnswer(t *testing.T) {
	t.Parallel()

	r := sampleReport()
	definition := countsOf(r.Counts, featureDefinition)
	want := featureCounts{
		Feature:     featureDefinition,
		Queries:     3,
		Disagree:    1,
		OursMissing: 1,
		Failed:      1,
		Items:       itemTally{Shared: 1, OursOnly: 1, TheirsOnly: 1},
		Ours:        sideTally{Answered: 1, Empty: 1, Timeout: 1},
		Theirs:      sideTally{Answered: 2, Empty: 1},
	}
	if definition != want {
		t.Errorf("definition = %+v, want %+v", definition, want)
	}
	hover := countsOf(r.Counts, featureHover)
	if hover.Failed != 1 || hover.Ours.Crashed != 1 || hover.Agree != 0 || hover.OursMissing != 0 {
		t.Errorf("hover = %+v, want one failure from a crash", hover)
	}
	if got := len(r.Counts.Features); got != len(featureSpecs) {
		t.Errorf("%d features reported, want %d", got, len(featureSpecs))
	}
}

func TestCompareRunsMeasuresOnlyResponses(t *testing.T) {
	t.Parallel()

	r := sampleReport()
	got := r.Ours.Latency[featureDefinition]
	if got.Samples != 2 || got.P50Micros != 1000 || got.MaxMicros != 2000 {
		t.Errorf("latency = %+v, want the two responses and not the timeout", got)
	}
	if empty := r.Ours.Latency[featureFolding]; empty.Samples != 0 || empty.MaxMicros != 0 {
		t.Errorf("a feature nobody asked has latency %+v", empty)
	}
}

func TestCompareRunsLimitsExamplesPerVerdict(t *testing.T) {
	t.Parallel()

	r := sampleReport()
	var got []string
	for _, e := range r.Examples {
		got = append(got, e.String())
	}
	want := []string{
		"definition a.nix:1:2: disagree",
		"definition a.nix:1:3: failed",
		"definition a.nix:1:4: ours missing",
		"hover a.nix:1:2: failed",
	}
	if !slices.Equal(got, want) {
		t.Errorf("examples = %q, want %q", got, want)
	}
	p := samplePlan()
	repeated := sideRun{answers: slices.Repeat([]answer{{outcome: outcomeAnswered, items: []string{"a"}}}, len(p.queries))}
	other := sideRun{answers: slices.Repeat([]answer{{outcome: outcomeAnswered, items: []string{"b"}}}, len(p.queries))}
	if limited := compareRuns(p, repeated, other, 1); len(limited.Examples) != 2 {
		t.Errorf("examples = %v, want one for each of the two features", limited.Examples)
	}
}
