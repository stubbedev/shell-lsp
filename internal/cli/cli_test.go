package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

type result struct {
	status int
	stdout string
	stderr string
}

func run(t *testing.T, dir, stdin string, args ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	status := Run(Env{
		Args:    args,
		Stdin:   strings.NewReader(stdin),
		Stdout:  &stdout,
		Stderr:  &stderr,
		Version: "1.2.3",
		Dir:     dir,
		Home:    filepath.Join(dir, "home"),
		Serve:   func() error { return errors.New("serving is not part of this test") },
	})
	return result{status: status, stdout: stdout.String(), stderr: stderr.String()}
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, text := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

var gccLine = regexp.MustCompile(`^(.*):(\d+):(\d+): \w+: .* \[(S[CL]\d+)\]$`)

func findings(t *testing.T, gcc string) []string {
	t.Helper()
	var out []string
	for line := range strings.SplitSeq(strings.TrimSpace(gcc), "\n") {
		if line == "" {
			continue
		}
		m := gccLine.FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("not a gcc finding: %q", line)
		}
		out = append(out, m[1]+":"+m[2]+":"+m[3]+" "+m[4])
	}
	return out
}

const unquoted = "#!/bin/bash\necho $1\n"

func TestCheckReportsOnlyTheInterpreterForForeignFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"status.sh": "#!/usr/bin/env python3\nCONF = Path.home() / \".conf\"\n"})
	got := run(t, dir, "", "check", "-f", "gcc", "status.sh")
	if want := []string{"status.sh:1:1 SC1071"}; !slices.Equal(findings(t, got.stdout), want) {
		t.Errorf("findings = %v, want %v", findings(t, got.stdout), want)
	}
	forced := run(t, dir, "", "check", "-f", "gcc", "--shell=bash", "status.sh")
	if !slices.Contains(findings(t, forced.stdout), "status.sh:2:17 SC1072") {
		t.Errorf("findings with --shell = %v, want the file parsed as bash", findings(t, forced.stdout))
	}
}

func TestCheckExitStatuses(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"clean.sh": "#!/bin/sh\necho hi\n", "bad.sh": unquoted, "rc/.shellcheckrc": "nonsense\n", "rc/x.sh": "#!/bin/sh\n"})
	for _, c := range []struct {
		args   []string
		status int
	}{
		{[]string{"check", "clean.sh"}, statusOK},
		{[]string{"check", "bad.sh"}, checkStatusFindings},
		{[]string{"check", "missing.sh", "bad.sh"}, checkStatusFiles},
		{[]string{"check"}, checkStatusSyntax},
		{[]string{"check", "--bogus", "clean.sh"}, checkStatusSyntax},
		{[]string{"check", "-e", "SCfoo", "clean.sh"}, checkStatusSyntax},
		{[]string{"check", "-f", "bogus", "clean.sh"}, checkStatusOptions},
		{[]string{"check", "-s", "fish", "clean.sh"}, checkStatusOptions},
		{[]string{"check", "-S", "loud", "clean.sh"}, checkStatusOptions},
		{[]string{"check", "rc/x.sh"}, checkStatusOptions},
		{[]string{"check", "--rcfile", "missing-rc", "clean.sh"}, checkStatusOptions},
		{[]string{"check", "-f", "quiet", "bad.sh"}, checkStatusFindings},
		{[]string{"check", "-S", "warning", "bad.sh"}, statusOK},
		{[]string{"check", "-e", "SC2086", "bad.sh"}, statusOK},
		{[]string{"check", "-i", "SC2034", "bad.sh"}, statusOK},
	} {
		if got := run(t, dir, "", c.args...); got.status != c.status {
			t.Errorf("%v exited %d, want %d\nstdout: %s\nstderr: %s", c.args, got.status, c.status, got.stdout, got.stderr)
		}
	}
}

func TestCheckFindsAndShortensPaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"proj/a.sh": unquoted, "proj/sub/b.bash": unquoted, "proj/notes.txt": unquoted,
		"proj/node_modules/x.sh": unquoted, "proj/.zshrc": "echo $1\n",
	})
	script := filepath.Join(dir, "proj", "tool")
	if err := os.WriteFile(script, []byte(unquoted), 0o755); err != nil {
		t.Fatal(err)
	}
	got := run(t, dir, "", "check", "-f", "gcc", "proj")
	want := []string{filepath.FromSlash("proj/a.sh:2:6 SC2086"), filepath.FromSlash("proj/sub/b.bash:2:6 SC2086"), filepath.FromSlash("proj/tool:2:6 SC2086")}
	if !slices.Equal(findings(t, got.stdout), want) || got.status != checkStatusFindings {
		t.Errorf("checking a directory = %d %v, want %v\n%s", got.status, findings(t, got.stdout), want, got.stderr)
	}
	twice := run(t, dir, "", "check", "-f", "gcc", "proj/a.sh", "proj", "./proj/a.sh")
	if n := strings.Count(twice.stdout, "a.sh:2:6"); n != 1 {
		t.Errorf("a file named twice was checked %d times:\n%s", n, twice.stdout)
	}
}

func TestCheckReadsStandardInput(t *testing.T) {
	t.Parallel()

	got := run(t, t.TempDir(), unquoted, "check", "-f", "gcc", "-")
	if !slices.Equal(findings(t, got.stdout), []string{"-:2:6 SC2086"}) {
		t.Errorf("stdin findings = %q", got.stdout)
	}
	guessed := run(t, t.TempDir(), "echo $1\n", "check", "-f", "gcc", "-")
	if !slices.Contains(findings(t, guessed.stdout), "-:1:1 SC2148") {
		t.Errorf("a script with no shell named must report SC2148: %q", guessed.stdout)
	}
	forced := run(t, t.TempDir(), "echo $1\n", "check", "-s", "bash", "-f", "gcc", "-")
	if slices.Contains(findings(t, forced.stdout), "-:1:1 SC2148") {
		t.Errorf("--shell must name the shell: %q", forced.stdout)
	}
}

func TestCheckReadsTheNearestShellcheckrc(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	script := "#!/bin/bash\nfoo=bar\necho $HOME\n"
	writeFiles(t, dir, map[string]string{
		"proj/.shellcheckrc":        "# team settings\ndisable=SC2034\nenable=require-variable-braces\n",
		"proj/sub/s.sh":             script,
		"other/s.sh":                script,
		"explicit.rc":               "disable=SC2086,SC2250\n",
		"home/.config/shellcheckrc": "disable=SC2086\n",
	})
	codes := func(args ...string) []string {
		got := run(t, dir, "", append([]string{"check", "-f", "gcc"}, args...)...)
		var out []string
		for _, f := range findings(t, got.stdout) {
			out = append(out, f[strings.LastIndexByte(f, ' ')+1:])
		}
		return out
	}
	if got := codes("proj/sub/s.sh"); !slices.Equal(got, []string{"SC2086", "SC2250"}) {
		t.Errorf("with the parent .shellcheckrc = %v", got)
	}
	if got := codes("--norc", "proj/sub/s.sh"); !slices.Equal(got, []string{"SC2034", "SC2086"}) {
		t.Errorf("with --norc = %v", got)
	}
	if got := codes("--rcfile", "explicit.rc", "proj/sub/s.sh"); !slices.Equal(got, []string{"SC2034"}) {
		t.Errorf("with --rcfile = %v", got)
	}
	if got := codes("other/s.sh"); !slices.Equal(got, []string{"SC2034"}) {
		t.Errorf("with the fallback in the config home = %v", got)
	}
}

func TestCheckRejectsMalformedShellcheckrc(t *testing.T) {
	t.Parallel()

	for _, rc := range []string{"disable\n", "colour=red\n", "shell=fish\n", "disable=SCxyz\n", "external-sources=maybe\n"} {
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{".shellcheckrc": rc, "a.sh": unquoted})
		if got := run(t, dir, "", "check", "a.sh"); got.status != checkStatusOptions || !strings.Contains(got.stderr, ".shellcheckrc:1:") {
			t.Errorf("rc %q: status %d, stderr %q", rc, got.status, got.stderr)
		}
	}
}

func TestCheckFollowsAndOptionallyReportsSourcedFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"src/main.sh": "#!/bin/bash\nsource ./lib.sh\necho \"$LIBVAR\"\n",
		"src/lib.sh":  "#!/bin/bash\nLIBVAR=1\necho $1\n",
	})
	plain := run(t, dir, "", "check", "-f", "gcc", "src/main.sh")
	if plain.status != statusOK {
		t.Errorf("main.sh, which reads a variable its sourced file sets, = %d %q", plain.status, plain.stdout)
	}
	sourced := run(t, dir, "", "check", "-a", "-f", "gcc", "src/main.sh")
	if got := findings(t, sourced.stdout); !slices.Contains(got, filepath.FromSlash("src/lib.sh:3:6 SC2086")) {
		t.Errorf("-a did not report the sourced file: %v", got)
	}
}

func TestCheckDiffAppliesTheFixAllEdits(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"a.sh": "#!/bin/bash\necho \"`date`\" $1\n", "b.sh": unquoted})
	got := run(t, dir, "", "check", "-f", "diff", "a.sh")
	want := "--- a/a.sh\n+++ b/a.sh\n@@ -1,2 +1,2 @@\n #!/bin/bash\n-echo \"`date`\" $1\n+echo \"$(date)\" $1\n\n"
	if got.stdout != want || got.status != checkStatusFindings {
		t.Errorf("diff = %d %q, want %q", got.status, got.stdout, want)
	}
	absolute := filepath.Join(dir, "a.sh")
	if got := run(t, dir, "", "check", "-f", "diff", absolute); !strings.HasPrefix(got.stdout, "--- "+absolute+"\n+++ "+absolute+"\n") {
		t.Errorf("diff of an absolute path = %q, want the path without a/ and b/ prefixes", got.stdout)
	}
	unfixable := run(t, dir, "", "check", "-f", "diff", "b.sh")
	if unfixable.stdout != "" || !strings.Contains(unfixable.stderr, "none were auto-fixable") || unfixable.status != checkStatusFindings {
		t.Errorf("unfixable diff = %+v", unfixable)
	}
}

func TestCheckWarnsAboutUnknownOptionalChecks(t *testing.T) {
	t.Parallel()

	got := run(t, t.TempDir(), "#!/bin/sh\necho hi\n", "check", "-o", "nosuch,require-variable-braces", "-")
	if got.status != statusOK || !strings.Contains(got.stderr, `"nosuch"`) || strings.Contains(got.stderr, "require-variable-braces") {
		t.Errorf("unknown optional check = %+v", got)
	}
	list := run(t, t.TempDir(), "", "check", "--list-optional")
	if !strings.Contains(list.stdout, "require-variable-braces\n    SC2250  A variable is expanded without braces.\n") || list.status != statusOK {
		t.Errorf("--list-optional = %+v", list)
	}
}

func TestCheckColorsOnlyWhenAsked(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"a.sh": unquoted})
	if got := run(t, dir, "", "check", "a.sh"); strings.Contains(got.stdout, "\x1b[") {
		t.Errorf("colored output without a terminal: %q", got.stdout)
	}
	if got := run(t, dir, "", "check", "-Calways", "a.sh"); !strings.Contains(got.stdout, "\x1b[32m") {
		t.Errorf("-Calways printed no color: %q", got.stdout)
	}
	if got := run(t, dir, "", "check", "--color=never", "a.sh"); strings.Contains(got.stdout, "\x1b[") {
		t.Errorf("--color=never printed color: %q", got.stdout)
	}
}

func TestRunDispatchesCommands(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if got := run(t, dir, ""); got.status != statusFailed || !strings.Contains(got.stderr, "serving is not part of this test") {
		t.Errorf("no arguments must serve LSP: %+v", got)
	}
	if got := run(t, dir, "", "--stdio"); got.status != statusFailed {
		t.Errorf("--stdio must serve LSP: %+v", got)
	}
	for _, args := range [][]string{{"version"}, {"--version"}, {"-version"}, {"check", "-V"}, {"fmt", "--version"}} {
		if got := run(t, dir, "", args...); got.status != statusOK || got.stdout != "shell-lsp 1.2.3\n" {
			t.Errorf("%v = %+v", args, got)
		}
	}
	for _, args := range [][]string{{"help"}, {"help", "check"}, {"help", "fmt"}, {"check", "--help"}, {"fmt", "-h"}} {
		if got := run(t, dir, "", args...); got.status != statusOK || (got.stdout == "" && got.stderr == "") {
			t.Errorf("%v = %+v", args, got)
		}
	}
	for _, args := range [][]string{{"lint"}, {"help", "lint"}, {"serve", "extra"}} {
		if got := run(t, dir, "", args...); got.status != statusUsage {
			t.Errorf("%v = %+v, want usage status", args, got)
		}
	}
}

func TestGNUOptionParsing(t *testing.T) {
	t.Parallel()

	opts, err := parseCheckArgs([]string{"-xaf", "gcc", "a.sh", "-Swarning", "--sev=error", "-e", "SC1,SC2", "--exclude=3", "-C", "--", "-b.sh"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.format.name != "gcc" || !opts.checkSourced || opts.severity != 1 || opts.color != colorAlways ||
		!opts.exclude.has(1) || !opts.exclude.has(3) || opts.exclude.has(4) || !slices.Equal(opts.operands, []string{"a.sh", "-b.sh"}) {
		t.Errorf("parsed options = %+v", opts)
	}
	for _, bad := range [][]string{{"-f"}, {"--format"}, {"-q"}, {"--ex", "x"}, {"--norc=yes"}, {"-W", "many"}} {
		if _, err := parseCheckArgs(bad); checkStatusOf(err) != checkStatusSyntax {
			t.Errorf("%v = %v, want a syntax error", bad, err)
		}
	}
}
