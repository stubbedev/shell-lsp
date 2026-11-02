package main

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"time"
)

type sideTally struct {
	Answered    int `json:"answered"`
	Empty       int `json:"empty"`
	Refused     int `json:"refused"`
	Unsupported int `json:"unsupported"`
	Silent      int `json:"silent"`
	Malformed   int `json:"malformed"`
	Timeout     int `json:"timeout"`
	Crashed     int `json:"crashed"`
}

func (t *sideTally) add(o outcome) {
	switch o {
	case outcomeAnswered:
		t.Answered++
	case outcomeEmpty:
		t.Empty++
	case outcomeRefused:
		t.Refused++
	case outcomeUnsupported:
		t.Unsupported++
	case outcomeSilent:
		t.Silent++
	case outcomeMalformed:
		t.Malformed++
	case outcomeTimeout:
		t.Timeout++
	case outcomeCrashed:
		t.Crashed++
	}
}

type itemTally struct {
	Shared     int `json:"shared"`
	OursOnly   int `json:"oursOnly"`
	TheirsOnly int `json:"theirsOnly"`
}

type featureCounts struct {
	Feature       feature   `json:"feature"`
	Queries       int       `json:"queries"`
	Agree         int       `json:"agree"`
	Disagree      int       `json:"disagree"`
	BothEmpty     int       `json:"bothEmpty"`
	OursMissing   int       `json:"oursMissing"`
	TheirsMissing int       `json:"theirsMissing"`
	Failed        int       `json:"failed"`
	Items         itemTally `json:"items"`
	Ours          sideTally `json:"ours"`
	Theirs        sideTally `json:"theirs"`
}

func (c *featureCounts) add(v verdict) {
	switch v {
	case verdictAgree:
		c.Agree++
	case verdictDisagree:
		c.Disagree++
	case verdictBothEmpty:
		c.BothEmpty++
	case verdictOursMissing:
		c.OursMissing++
	case verdictTheirsMissing:
		c.TheirsMissing++
	case verdictFailed:
		c.Failed++
	}
}

type scope struct {
	Project   string   `json:"project"`
	Seed      uint64   `json:"seed"`
	Positions int      `json:"positionsPerFile"`
	FileLimit int      `json:"fileLimit"`
	Files     int      `json:"files"`
	Theirs    identity `json:"theirs"`
}

type counts struct {
	Scope    scope           `json:"scope"`
	Skipped  []string        `json:"skipped"`
	Features []featureCounts `json:"features"`
}

type latencySummary struct {
	Feature   feature `json:"feature"`
	Samples   int     `json:"samples"`
	P50Micros int64   `json:"p50Micros"`
	P90Micros int64   `json:"p90Micros"`
	P99Micros int64   `json:"p99Micros"`
	MaxMicros int64   `json:"maxMicros"`
}

type performance struct {
	Server       identity         `json:"server"`
	Restarts     int              `json:"restarts"`
	PeakRSSBytes *int64           `json:"peakRssBytes"`
	IdleRSSBytes *int64           `json:"idleRssBytes"`
	Latency      []latencySummary `json:"latency"`
}

type example struct {
	Feature feature `json:"feature"`
	File    string  `json:"file"`
	Line    uint32  `json:"line"`
	Column  uint32  `json:"column"`
	Verdict verdict `json:"verdict"`
}

func (e example) String() string {
	if e.Feature.spec().positional {
		return fmt.Sprintf("%s %s:%d:%d: %s", e.Feature, e.File, e.Line, e.Column, e.Verdict)
	}
	return fmt.Sprintf("%s %s: %s", e.Feature, e.File, e.Verdict)
}

type report struct {
	Counts   counts      `json:"counts"`
	Ours     performance `json:"ours"`
	Theirs   performance `json:"theirs"`
	Examples []example   `json:"examples"`
}

func judge(spec featureSpec, ours, theirs answer) verdict {
	oursAnswered, theirsAnswered := ours.outcome == outcomeAnswered, theirs.outcome == outcomeAnswered
	switch {
	case ours.outcome.failed() || theirs.outcome.failed():
		return verdictFailed
	case oursAnswered && theirsAnswered:
		if spec.agrees(ours.items, theirs.items) {
			return verdictAgree
		}
		return verdictDisagree
	case oursAnswered:
		return verdictTheirsMissing
	case theirsAnswered:
		return verdictOursMissing
	}
	return verdictBothEmpty
}

func overlap(a, b []string) itemTally {
	var t itemTally
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch c := cmp.Compare(a[i], b[j]); {
		case c == 0:
			t.Shared++
			i++
			j++
		case c < 0:
			t.OursOnly++
			i++
		default:
			t.TheirsOnly++
			j++
		}
	}
	t.OursOnly += len(a) - i
	t.TheirsOnly += len(b) - j
	return t
}

func (t *itemTally) merge(o itemTally) {
	t.Shared += o.Shared
	t.OursOnly += o.OursOnly
	t.TheirsOnly += o.TheirsOnly
}

func percentile(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := (p*len(sorted) + 99) / 100
	return sorted[max(rank, 1)-1]
}

func summarize(f feature, samples []time.Duration) latencySummary {
	slices.Sort(samples)
	s := latencySummary{Feature: f, Samples: len(samples)}
	if len(samples) == 0 {
		return s
	}
	s.P50Micros = percentile(samples, 50).Microseconds()
	s.P90Micros = percentile(samples, 90).Microseconds()
	s.P99Micros = percentile(samples, 99).Microseconds()
	s.MaxMicros = samples[len(samples)-1].Microseconds()
	return s
}

func latencies(queries []query, answers []answer) []latencySummary {
	samples := make([][]time.Duration, len(featureSpecs))
	for i, q := range queries {
		if answers[i].responded() {
			samples[q.feature] = append(samples[q.feature], answers[i].latency)
		}
	}
	out := make([]latencySummary, 0, len(samples))
	for _, f := range allFeatures() {
		out = append(out, summarize(f, samples[f]))
	}
	return out
}

func performanceOf(p plan, run sideRun) performance {
	return performance{Server: run.identity, Restarts: run.restarts, PeakRSSBytes: run.peakRSS, IdleRSSBytes: run.idleRSS, Latency: latencies(p.queries, run.answers)}
}

func compareRuns(p plan, ours, theirs sideRun, exampleLimit int) report {
	features := make([]featureCounts, len(featureSpecs))
	for _, f := range allFeatures() {
		features[f].Feature = f
	}
	type exampleKey struct {
		feature feature
		verdict verdict
	}
	shown := map[exampleKey]int{}
	examples := []example{}
	for i, q := range p.queries {
		o, t := ours.answers[i], theirs.answers[i]
		spec := q.feature.spec()
		v := judge(spec, o, t)
		c := &features[q.feature]
		c.Queries++
		c.add(v)
		c.Ours.add(o.outcome)
		c.Theirs.add(t.outcome)
		if v == verdictAgree || v == verdictDisagree {
			c.Items.merge(overlap(o.items, t.items))
		}
		key := exampleKey{q.feature, v}
		if v == verdictAgree || v == verdictBothEmpty || shown[key] >= exampleLimit {
			continue
		}
		shown[key]++
		examples = append(examples, example{Feature: q.feature, File: p.docs[q.file].rel, Line: q.position.Line + 1, Column: q.position.Character + 1, Verdict: v})
	}
	skipped := p.skipped
	if skipped == nil {
		skipped = []string{}
	}
	return report{
		Counts: counts{
			Scope: scope{
				Project:   filepath.Base(p.root),
				Seed:      p.settings.seed,
				Positions: p.settings.positions,
				FileLimit: p.settings.fileLimit,
				Files:     len(p.docs),
				Theirs:    theirs.identity,
			},
			Skipped:  skipped,
			Features: features,
		},
		Ours:     performanceOf(p, ours),
		Theirs:   performanceOf(p, theirs),
		Examples: examples,
	}
}
