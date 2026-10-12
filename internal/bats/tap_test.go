package bats

import (
	"reflect"
	"strings"
	"testing"
)

func parse(tap string) (Report, int) {
	var p Parser
	results := 0
	for line := range strings.Lines(tap) {
		if p.Line(strings.TrimSuffix(line, "\n")) {
			results++
		}
	}
	return p.Report(), results
}

func TestParsePassingRun(t *testing.T) {
	t.Parallel()

	report, signalled := parse("1..2\nok 1 adds numbers\nok 2 - handles \\# signs\n")
	want := Report{Plan: &Plan{Tests: 2}, Results: []Result{
		{Number: 1, Name: "adds numbers", Status: Passed},
		{Number: 2, Name: "handles # signs", Status: Passed},
	}}
	if !reflect.DeepEqual(report, want) || signalled != 2 {
		t.Fatalf("report = %+v (%d results signalled), want %+v", report, signalled, want)
	}
	if !report.OK() || report.Summary() != "2 passed, 0 failed, 0 skipped" {
		t.Errorf("OK = %t, summary = %q", report.OK(), report.Summary())
	}
}

func TestParseFailureWithDiagnostics(t *testing.T) {
	t.Parallel()

	tap := "1..2\r\n" +
		"not ok 1 compares output\n" +
		"# (from function `assert_equal' in file test/helper.bash, line 4,\n" +
		"#  in test file test/suite.bats, line 9)\n" +
		"#   `assert_equal \"$output\" 3' failed\n" +
		"# expected: 3\n" +
		"ok 2 still runs\n"
	report, _ := parse(tap)
	if len(report.Results) != 2 {
		t.Fatalf("results = %+v", report.Results)
	}
	got := report.Results[0]
	want := Result{
		Number: 1,
		Name:   "compares output",
		Status: Failed,
		Output: []string{"  `assert_equal \"$output\" 3' failed", "expected: 3"},
		Frames: []Frame{{File: "test/helper.bash", Line: 4}, {File: "test/suite.bats", Line: 9}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("failure = %+v\nwant      %+v", got, want)
	}
	if report.OK() || report.Tally() != (Tally{Passed: 1, Failed: 1}) {
		t.Errorf("OK = %t, tally = %+v", report.OK(), report.Tally())
	}
}

func TestParseSkipAndTodo(t *testing.T) {
	t.Parallel()

	report, _ := parse("1..4\nok 1 needs docker # skip no docker here\nok 2 bare skip # SKIP\nnot ok 3 unfinished # TODO write it\nok 4 not a directive # just a comment\n")
	want := []Result{
		{Number: 1, Name: "needs docker", Status: Skipped, Reason: "no docker here"},
		{Number: 2, Name: "bare skip", Status: Skipped},
		{Number: 3, Name: "unfinished", Status: Todo, Reason: "write it"},
		{Number: 4, Name: "not a directive # just a comment", Status: Passed},
	}
	if !reflect.DeepEqual(report.Results, want) {
		t.Errorf("results = %+v\nwant      %+v", report.Results, want)
	}
	if !report.OK() || report.Summary() != "1 passed, 0 failed, 2 skipped, 1 todo" {
		t.Errorf("OK = %t, summary = %q", report.OK(), report.Summary())
	}
}

func TestParseBailOut(t *testing.T) {
	t.Parallel()

	report, _ := parse("1..3\nok 1 first\nBail out! database unreachable\nnot ok 2 ignored after bailing\n")
	if report.Bail == nil || report.Bail.Reason != "database unreachable" || len(report.Results) != 1 {
		t.Fatalf("report = %+v", report)
	}
	if report.OK() || report.Summary() != "1 passed, 0 failed, 0 skipped; planned 3 tests but 1 reported; bailed out: database unreachable" {
		t.Errorf("OK = %t, summary = %q", report.OK(), report.Summary())
	}
}

func TestParsePlanMismatch(t *testing.T) {
	t.Parallel()

	report, _ := parse("1..3\nok 1 first\nok 2 second\n")
	if !report.PlanMismatch() || report.OK() || report.Summary() != "2 passed, 0 failed, 0 skipped; planned 3 tests but 2 reported" {
		t.Errorf("mismatch = %t, OK = %t, summary = %q", report.PlanMismatch(), report.OK(), report.Summary())
	}

	trailing, _ := parse("ok 1 first\n1..1\n")
	if trailing.PlanMismatch() || !trailing.OK() {
		t.Errorf("a trailing plan = %+v, want it honoured", trailing)
	}

	unplanned, _ := parse("ok 1 first\n")
	if unplanned.OK() || unplanned.Summary() != "1 passed, 0 failed, 0 skipped; no TAP plan was reported" {
		t.Errorf("unplanned OK = %t, summary = %q", unplanned.OK(), unplanned.Summary())
	}
}

func TestParseIgnoresNoise(t *testing.T) {
	t.Parallel()

	report, _ := parse("TAP version 13\n# before any test\nokay then\n1..1\nok\n")
	if !reflect.DeepEqual(report, Report{Plan: &Plan{Tests: 1}, Results: []Result{{Number: 1, Status: Passed}}}) {
		t.Errorf("report = %+v", report)
	}
	if report.Empty() || !(Report{}).Empty() {
		t.Errorf("Empty() = %t for a report with a plan, %t for a blank one", report.Empty(), (Report{}).Empty())
	}
}
