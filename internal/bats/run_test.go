package bats

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/platform"
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
	inv := Invocation{Runner: Command{Program: exe}, Dir: dir, File: "suite.bats", Test: []string{name}}
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
	_, err := Run(t.Context(), Invocation{Runner: Command{Program: exe}, Dir: dir, File: "x.bats"}, func(Report) {})
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
	_, err := Run(ctx, Invocation{Runner: Command{Program: exe}, Dir: dir, File: "x.bats"}, func(Report) {})
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
	if got, err := Locate(dir, Environment{}); err != nil || !reflect.DeepEqual(got, Command{Program: local}) {
		t.Errorf("Locate() = %+v, %v, want %q", got, err, local)
	}
}

func TestLocateRejectsANonExecutableRunner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test", "bats", "bin", "bats")
	writeExecutable(t, path, "exit 0\n")
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Locate(dir, Environment{}); err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("Locate() error = %v, want it to name %s", err, path)
	}
}

func TestLocateSearchesPath(t *testing.T) {
	bin := t.TempDir()
	onPath := filepath.Join(bin, "bats")
	writeExecutable(t, onPath, "exit 0\n")
	if got, err := Locate(t.TempDir(), Environment{Path: []string{"", t.TempDir(), bin}}); err != nil || !reflect.DeepEqual(got, Command{Program: onPath}) {
		t.Errorf("Locate() = %+v, %v, want %q", got, err, onPath)
	}

	dir := t.TempDir()
	_, err := Locate(dir, Environment{Path: []string{t.TempDir()}})
	if err == nil || err.Error() != "bats not found: looked for test/bats/bin/bats, node_modules/.bin/bats, test/libs/bats/bin/bats under "+dir+", then for bats on PATH" {
		t.Errorf("Locate() error = %v", err)
	}
}

func TestInvocationPrependsTheRunnerArguments(t *testing.T) {
	t.Parallel()

	inv := Invocation{Runner: Command{Program: "bash.exe", Args: []string{"C:/p/bats"}}, File: "test/a.bats", Test: []string{"x"}}
	want := []string{"C:/p/bats", "--tap", "--filter", "^x$", "test/a.bats"}
	if got := inv.Args(); !reflect.DeepEqual(got, want) {
		t.Errorf("Args() = %q, want %q", got, want)
	}
	if got := inv.Args(); !reflect.DeepEqual(got, want) || len(inv.Runner.Args) != 1 {
		t.Errorf("Args() changed the runner: %q", inv.Runner.Args)
	}
}

func windowsHost(t *testing.T, pathDirs []string, systemRoot string) host {
	t.Helper()

	return host{
		flavour:    platform.Windows,
		pathDirs:   pathDirs,
		systemRoot: systemRoot,
		lookPath:   func(string) (string, error) { return "", exec.ErrNotFound },
		isFile:     isFile,
	}
}

func touch(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLocateRunsABatsScriptThroughBashOnWindows(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	system32 := filepath.Join(root, "Windows", "System32")
	gitBin := filepath.Join(root, "Git", "bin")
	touch(t, filepath.Join(system32, "bash.exe"))
	touch(t, filepath.Join(gitBin, "bash.exe"))
	project := filepath.Join(root, "proj")
	script := filepath.Join(project, "test", "bats", "bin", "bats")
	touch(t, script)

	h := windowsHost(t, []string{system32, "", gitBin}, filepath.Join(root, "Windows"))
	got, err := h.locate(project)
	want := Command{Program: filepath.Join(gitBin, "bash.exe"), Args: []string{strings.ReplaceAll(script, `\`, "/")}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("locate() = %+v, %v, want %+v", got, err, want)
	}

	onPath := filepath.Join(root, "tools")
	touch(t, filepath.Join(onPath, "bats"))
	h = windowsHost(t, []string{onPath, gitBin}, filepath.Join(root, "Windows"))
	got, err = h.locate(t.TempDir())
	want = Command{Program: filepath.Join(gitBin, "bash.exe"), Args: []string{strings.ReplaceAll(filepath.Join(onPath, "bats"), `\`, "/")}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("locate() on PATH = %+v, %v, want %+v", got, err, want)
	}
}

func TestLocateRefusesTheWSLBashOnWindows(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	system32 := filepath.Join(root, "Windows", "System32")
	touch(t, filepath.Join(system32, "bash.exe"))
	project := filepath.Join(root, "proj")
	touch(t, filepath.Join(project, "node_modules", ".bin", "bats"))

	h := windowsHost(t, []string{system32}, filepath.Join(root, "Windows"))
	if _, err := h.locate(project); err == nil || !strings.Contains(err.Error(), "Git for Windows or MSYS2") {
		t.Errorf("locate() error = %v, want it to ask for Git Bash or MSYS2", err)
	}
}

func TestLocatePrefersAWindowsLauncherOnPath(t *testing.T) {
	t.Parallel()

	h := windowsHost(t, nil, "")
	h.lookPath = func(name string) (string, error) {
		if name == command {
			return `C:\npm\bats.cmd`, nil
		}
		return "", exec.ErrNotFound
	}
	if got, err := h.locate(t.TempDir()); err != nil || !reflect.DeepEqual(got, Command{Program: `C:\npm\bats.cmd`}) {
		t.Errorf("locate() = %+v, %v, want the bats.cmd launcher", got, err)
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
