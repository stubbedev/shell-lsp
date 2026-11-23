package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/gitrepo"
)

const (
	ugly   = "if true;then\necho  hi\nfi\n"
	pretty = "if true; then\n\techo hi\nfi\n"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestFmtPrintsListsAndDiffs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"ugly.sh": ugly, "good.sh": pretty, "broken.sh": "if true; then\n"})
	for _, c := range []struct {
		args   []string
		status int
		stdout string
	}{
		{[]string{"fmt", "ugly.sh"}, statusOK, pretty},
		{[]string{"fmt", "-l", "ugly.sh", "good.sh"}, statusFailed, "ugly.sh\n"},
		{[]string{"fmt", "-l=0", "ugly.sh", "good.sh"}, statusFailed, "ugly.sh\x00"},
		{[]string{"fmt", "-l", "good.sh"}, statusOK, ""},
		{[]string{"fmt", "-d", "ugly.sh"}, statusFailed, "diff ugly.sh.orig ugly.sh\n--- ugly.sh.orig\n+++ ugly.sh\n@@ -1,3 +1,3 @@\n-if true;then\n-echo  hi\n+if true; then\n+\techo hi\n fi\n"},
		{[]string{"fmt", "-i", "2", "ugly.sh"}, statusOK, "if true; then\n  echo hi\nfi\n"},
		{[]string{"fmt", "-mn", "ugly.sh"}, statusOK, "if true;then\necho hi\nfi\n"},
		{[]string{"fmt", "broken.sh"}, statusFailed, ""},
		{[]string{"fmt", "missing.sh"}, statusFailed, ""},
		{[]string{"fmt", "-ln", "fish", "ugly.sh"}, statusUsage, ""},
		{[]string{"fmt", "-zz"}, statusUsage, ""},
		{[]string{"fmt", "-f", "."}, statusOK, "broken.sh\ngood.sh\nugly.sh\n"},
		{[]string{"fmt", "-l", "ugly.sh", "./ugly.sh", "."}, statusFailed, "ugly.sh\n"},
	} {
		got := run(t, dir, "", c.args...)
		if got.status != c.status || got.stdout != c.stdout {
			t.Errorf("%v = %d %q, want %d %q (stderr %q)", c.args, got.status, got.stdout, c.status, c.stdout, got.stderr)
		}
	}
	if got := run(t, dir, "", "fmt", "broken.sh"); !strings.HasPrefix(got.stderr, "broken.sh:1:") {
		t.Errorf("a syntax error is reported as %q, want file:line:col", got.stderr)
	}
}

func TestFmtPosixFlagIsShortForPosixLanguage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const script = "[[ -n $x ]]&&echo  hi\n"
	long := run(t, dir, script, "fmt", "-ln", "posix")
	short := run(t, dir, script, "fmt", "-p")
	if short != long || long.status != statusOK || long.stdout != "[[ -n $x ]] && echo hi\n" {
		t.Errorf("fmt -p = %+v, fmt -ln posix = %+v; want the same formatted script", short, long)
	}
}

func TestFmtFormatsStandardInput(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if got := run(t, dir, ugly, "fmt"); got.stdout != pretty || got.status != statusOK {
		t.Errorf("stdin = %+v", got)
	}
	if got := run(t, dir, ugly, "fmt", "-l", "-"); got.stdout != "<standard input>\n" || got.status != statusFailed {
		t.Errorf("stdin -l = %+v", got)
	}
	if got := run(t, dir, ugly, "fmt", "-w"); got.status != statusFailed || !strings.Contains(got.stderr, "standard input") {
		t.Errorf("stdin -w = %+v", got)
	}
	writeFiles(t, dir, map[string]string{".editorconfig": "root = true\n[*.sh]\nindent_style = space\nindent_size = 3\n"})
	if got := run(t, dir, ugly, "fmt", "--filename", "named.sh"); got.stdout != "if true; then\n   echo hi\nfi\n" {
		t.Errorf("--filename did not pick up .editorconfig: %q", got.stdout)
	}
}

func TestFmtNamesStdinToChooseItsLanguage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if got := run(t, dir, "{a=1;}\n", "fmt", "--filename", "x.nix"); got.stdout != "{ a = 1; }\n" || got.status != statusOK {
		t.Errorf("fmt --filename x.nix = %+v", got)
	}
	if got := run(t, dir, "all:\n\techo hi\n", "fmt", "--filename", "Makefile"); got.stdout != "all:\n\techo hi\n" || got.status != statusOK {
		t.Errorf("fmt --filename Makefile = %+v", got)
	}
	if got := run(t, dir, "FROM x\n", "fmt", "--filename", "Dockerfile"); got.status != statusFailed || !strings.Contains(got.stderr, "formats only") {
		t.Errorf("fmt --filename Dockerfile = %+v", got)
	}
}

func TestFmtWriteReplacesFilesAtomically(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "w.sh")
	if err := os.WriteFile(path, []byte(ugly), 0o751); err != nil {
		t.Fatal(err)
	}
	if got := run(t, dir, "", "fmt", "-l", "-w", "w.sh"); got.status != statusOK || got.stdout != "w.sh\n" {
		t.Errorf("-l -w = %+v", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	wantMode := os.FileMode(0o751)
	if runtime.GOOS == "windows" {
		wantMode = 0o666
	}
	if got := readFile(t, path); got != pretty || info.Mode().Perm() != wantMode {
		t.Errorf("-w wrote %q with mode %o, want the formatted text with mode %o", got, info.Mode().Perm(), wantMode)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("-w left files behind: %v", entries)
	}
}

func TestFmtHonoursEditorConfigUnlessStyleFlagsAreGiven(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		".editorconfig": "root = true\n[*.sh]\nindent_style = space\nindent_size = 4\n[vendor/**]\nignore = true\n",
		"a.sh":          ugly,
		"vendor/v.sh":   ugly,
	})
	if got := run(t, dir, "", "fmt", "a.sh"); got.stdout != "if true; then\n    echo hi\nfi\n" {
		t.Errorf(".editorconfig ignored: %q", got.stdout)
	}
	if got := run(t, dir, "", "fmt", "-ci", "a.sh"); got.stdout != pretty {
		t.Errorf("a printer flag did not replace .editorconfig: %q", got.stdout)
	}
	if got := run(t, dir, "", "fmt", "-l", "."); got.stdout != "a.sh\n" {
		t.Errorf("walking did not apply ignore = true: %q", got.stdout)
	}
	if got := run(t, dir, "", "fmt", "-l", "vendor/v.sh"); got.stdout != "vendor/v.sh\n" {
		t.Errorf("a file named explicitly must be formatted: %q", got.stdout)
	}
	if got := run(t, dir, "", "fmt", "--apply-ignore", "-l", "vendor/v.sh"); got.stdout != "" {
		t.Errorf("--apply-ignore did not skip the ignored file: %q", got.stdout)
	}
}

func TestFmtFormatsNixFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"a.nix": "{a=1;b=[1 2];}\n", "good.nix": "{ a = 1; }\n", "broken.nix": "{ a = ; }\n", ".editorconfig": "[*]\nindent_size = 8\nindent_style = tab\n"})
	want := "{\n  a = 1;\n  b = [\n    1\n    2\n  ];\n}\n"
	if got := run(t, dir, "", "fmt", "a.nix"); got.status != statusOK || got.stdout != want {
		t.Errorf("fmt a.nix = %d %q (stderr %q), want %q", got.status, got.stdout, got.stderr, want)
	}
	if got := run(t, dir, "", "fmt", "-l", "a.nix", "good.nix"); got.status != statusFailed || got.stdout != "a.nix\n" {
		t.Errorf("fmt -l = %d %q (stderr %q)", got.status, got.stdout, got.stderr)
	}
	got := run(t, dir, "", "fmt", "-w", "broken.nix")
	if got.status != statusFailed || !strings.Contains(got.stderr, "1:7") {
		t.Errorf("fmt -w broken.nix = %d, stderr %q, want the position of the syntax error", got.status, got.stderr)
	}
	if after := readFile(t, filepath.Join(dir, "broken.nix")); after != "{ a = ; }\n" {
		t.Errorf("a refused file was rewritten to %q", after)
	}
}

func TestFmtSkipsNamedFilesOfOtherLanguages(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"t.py":   "import os\n\ndef main(argv):\n    print(os.getcwd())\n",
		"b.hcl":  "resource \"x\" \"y\" {\n  name = \"z\"\n}\n",
		"script": ugly,
		"run.py": "#!/bin/bash\n" + ugly,
	})
	if got := run(t, dir, "", "fmt", "-l", "t.py", "b.hcl"); got.status != statusOK || got.stdout != "" || got.stderr != "" {
		t.Errorf("formatting only files of other languages = %+v", got)
	}
	if got := run(t, dir, "", "fmt", "-l", "t.py", "b.hcl", "script", "run.py"); got.status != statusFailed || got.stdout != "script\nrun.py\n" || got.stderr != "" {
		t.Errorf("fmt -l over mixed files = %+v, want the extensionless script and the bash .py listed", got)
	}
	if got := run(t, dir, "", "fmt", "-f", "t.py", "script", "run.py"); got.status != statusOK || got.stdout != "script\nrun.py\n" {
		t.Errorf("fmt -f over mixed files = %+v", got)
	}
	if got := run(t, dir, "", "fmt", "-ln", "bash", "t.py"); got.status != statusFailed || !strings.HasPrefix(got.stderr, "t.py:") {
		t.Errorf("fmt -ln bash t.py = %+v, want it parsed as bash", got)
	}
}

func TestFmtWalkHonoursTheGitConfigVariables(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		".git/HEAD":               "ref: refs/heads/main\n",
		".git/objects/info/packs": "",
		".git/refs/heads/main":    "0123456789abcdef0123456789abcdef01234567\n",
		"kept/a.sh":               ugly,
		"skipped/b.sh":            ugly,
		"home/.gitconfig":         "[core]\n\texcludesFile = ~/nowhere\n",
		"elsewhere/ignore":        "skipped/\n",
	})
	var stdout, stderr bytes.Buffer
	status := Run(Env{
		Args:   []string{"fmt", "-l", "."},
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: &stderr,
		Dir:    dir,
		Home:   filepath.Join(dir, "home"),
		Git: gitrepo.Process{Variables: map[string]string{
			"GIT_CONFIG_COUNT":   "1",
			"GIT_CONFIG_KEY_0":   "core.excludesFile",
			"GIT_CONFIG_VALUE_0": filepath.Join(dir, "elsewhere", "ignore"),
		}},
		Serve: func() error { return errors.New("serving is not part of this test") },
	})
	if want := filepath.Join("kept", "a.sh") + "\n"; status != statusFailed || stdout.String() != want {
		t.Errorf("fmt -l . = %d, %q (%s); want %d, %q", status, stdout.String(), stderr.String(), statusFailed, want)
	}
}

func TestFmtWalkFindsTheRepositoryWhereGitDirPoints(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"work/.git/HEAD":               "ref: refs/heads/main\n",
		"work/.git/objects/info/packs": "",
		"work/.git/refs/heads/main":    "0123456789abcdef0123456789abcdef01234567\n",
		"work/kept/a.sh":               ugly,
		"work/skipped/b.sh":            ugly,
		"store/git/HEAD":               "ref: refs/heads/main\n",
		"store/git/objects/info/packs": "",
		"store/git/refs/heads/main":    "0123456789abcdef0123456789abcdef01234567\n",
		"store/git/info/exclude":       "/skipped/\n",
	})
	fmtList := func(vars map[string]string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		status := Run(Env{
			Args:   []string{"fmt", "-l", "."},
			Stdin:  strings.NewReader(""),
			Stdout: &stdout,
			Stderr: &stderr,
			Dir:    filepath.Join(dir, "work"),
			Home:   filepath.Join(dir, "home"),
			Git:    gitrepo.Process{Variables: vars},
			Serve:  func() error { return errors.New("serving is not part of this test") },
		})
		return status, stdout.String(), stderr.String()
	}
	kept, skipped := filepath.Join("kept", "a.sh"), filepath.Join("skipped", "b.sh")
	if status, stdout, stderr := fmtList(nil); status != statusFailed || stdout != kept+"\n"+skipped+"\n" {
		t.Errorf("fmt -l . without GIT_DIR = %d, %q (%s); want both files", status, stdout, stderr)
	}
	gitDir := filepath.Join("..", "store", "git")
	if status, stdout, stderr := fmtList(map[string]string{"GIT_DIR": gitDir}); status != statusFailed || stdout != kept+"\n" {
		t.Errorf("fmt -l . with GIT_DIR=%s = %d, %q (%s); want %q", gitDir, status, stdout, stderr, kept)
	}
	if status, _, stderr := fmtList(map[string]string{"GIT_DIR": filepath.Join(dir, "missing")}); status != statusFailed || !strings.Contains(stderr, "GIT_DIR") {
		t.Errorf("fmt -l . with a missing GIT_DIR = %d, %q; want an error naming GIT_DIR", status, stderr)
	}
}
