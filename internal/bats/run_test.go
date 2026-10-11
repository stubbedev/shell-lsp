package bats

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("the fake bats runner is a POSIX shell script")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestRunPassesArgvAndStreamsResults(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "bats")
	writeExecutable(t, exe, `pwd > argv.txt
for arg in "$@"; do printf '%s\n' "$arg" >> argv.txt; done
echo '1..2'
echo 'ok 1 first'
echo 'not ok 2 second (x|y)'
echo '# (in test file suite.bats, line 3)'
exit 1
`)
	name := "second (x|y)"
	inv := Invocation{Executable: exe, Dir: dir, File: filepath.Join(dir, "suite.bats"), Test: []string{name}}
	var progress []int
	report, err := Run(t.Context(), inv, func(r Report) { progress = append(progress, len(r.Results)) })
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.Tally() != (Tally{Passed: 1, Failed: 1}) || len(progress) != 2 || progress[1] != 2 {
		t.Errorf("report = %+v, progress = %v", report, progress)
	}
	argv, err := os.ReadFile(filepath.Join(dir, "argv.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{dir, "--tap", "--filter", `^second \(x\|y\)$`, inv.File}, "\n") + "\n"
	if string(argv) != want {
		t.Errorf("cwd and argv = %q, want %q", argv, want)
	}
}

func TestRunWithoutTAPReportsStderr(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "bats")
	writeExecutable(t, exe, "echo 'bats: unknown option' >&2\nexit 2\n")
	_, err := Run(t.Context(), Invocation{Executable: exe, Dir: dir, File: filepath.Join(dir, "x.bats")}, func(Report) {})
	if err == nil || !strings.Contains(err.Error(), "exited with status 2 without reporting any test: bats: unknown option") {
		t.Errorf("Run() error = %v", err)
	}
}

func TestRunStopsOnCancellation(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "bats")
	writeExecutable(t, exe, "echo '1..1'\nsleep 30 &\nsleep 30\n")
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Run(ctx, Invocation{Executable: exe, Dir: dir, File: filepath.Join(dir, "x.bats")}, func(Report) {})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Run() error = %v, want the deadline", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("Run() took %s after cancellation", elapsed)
	}
}

func TestLocatePrefersTheProjectRunner(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, "node_modules", ".bin", "bats")
	writeExecutable(t, local, "exit 0\n")
	if got, err := Locate(dir); err != nil || got != local {
		t.Errorf("Locate() = %q, %v, want %q", got, err, local)
	}
}

func TestLocateRejectsANonExecutableRunner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test", "bats", "bin", "bats")
	writeExecutable(t, path, "exit 0\n")
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Locate(dir); err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("Locate() error = %v, want it to name %s", err, path)
	}
}

func TestLocateSearchesPath(t *testing.T) {
	bin := t.TempDir()
	onPath := filepath.Join(bin, "bats")
	writeExecutable(t, onPath, "exit 0\n")
	t.Setenv("PATH", bin)
	if got, err := Locate(t.TempDir()); err != nil || got != onPath {
		t.Errorf("Locate() = %q, %v, want %q", got, err, onPath)
	}

	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	_, err := Locate(dir)
	if err == nil || err.Error() != "bats not found: looked for test/bats/bin/bats, node_modules/.bin/bats, test/libs/bats/bin/bats under "+dir+", then for bats on PATH" {
		t.Errorf("Locate() error = %v", err)
	}
}

func TestSegmentsMatchResolvedNames(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		segments []string
		name     string
		want     bool
	}{
		{[]string{"exact (x|y)"}, "exact (x|y)", true},
		{[]string{"exact"}, "exactly", false},
		{[]string{"uses ", ""}, "uses /home/u", true},
		{[]string{"uses ", ""}, "abuses x", false},
		{[]string{"", " then ", "!"}, "a then b!", true},
		{[]string{"", " then ", "!"}, "a then b", false},
		{[]string{"ab", "b"}, "ab", false},
		{[]string{"ab", "b"}, "abb", true},
	} {
		if got := Matches(tc.segments, tc.name); got != tc.want {
			t.Errorf("Matches(%q, %q) = %v, want %v", tc.segments, tc.name, got, tc.want)
		}
	}
	if got := Filter([]string{"run ", " (fast)"}); got != `^run .* \(fast\)$` {
		t.Errorf("Filter = %q", got)
	}
}
