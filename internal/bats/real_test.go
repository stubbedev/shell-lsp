package bats

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

const realSuite = `#!/usr/bin/env bats

check() {
  [ "$1" = expected ]
}

@test "passes" {
  true
}

@test "fails in a helper (x|y)" {
  check actual
}

@test "fails directly" {
  [ 1 -eq 2 ]
}

@test "is skipped" {
  skip "not today"
}
`

func TestRealBats(t *testing.T) {
	exe, err := exec.LookPath("bats")
	if err != nil {
		t.Skip("bats is not installed; the nix dev shell provides it")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "suite.bats")
	if err := os.WriteFile(file, []byte(realSuite), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Run(t.Context(), Invocation{Executable: exe, Dir: dir, File: file}, func(Report) {})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := report.Tally(); got != (Tally{Passed: 1, Failed: 2, Skipped: 1}) || report.PlanMismatch() {
		t.Fatalf("tally = %+v, plan mismatch %v, report %+v", got, report.PlanMismatch(), report)
	}
	lines := map[string]int{"passes": 7, "fails in a helper (x|y)": 11, "fails directly": 15, "is skipped": 19}
	failures := Failures(report, file, dir, func(name string) (int, bool) {
		line, ok := lines[name]
		return line, ok
	})
	var got []Failure
	for _, f := range failures {
		got = append(got, Failure{Test: f.Test, Line: f.Line})
	}
	want := []Failure{{Test: "fails in a helper (x|y)", Line: 4}, {Test: "fails directly", Line: 16}}
	if !slices.Equal(got, want) {
		t.Errorf("failure lines = %+v, want the failing commands %+v", got, want)
	}

	name := "fails in a helper (x|y)"
	single, err := Run(t.Context(), Invocation{Executable: exe, Dir: dir, File: file, Test: &name}, func(Report) {})
	if err != nil {
		t.Fatalf("Run(single) error = %v", err)
	}
	if got := single.Tally(); got != (Tally{Failed: 1}) {
		t.Errorf("filtered run tally = %+v, want only the named test", got)
	}
}
