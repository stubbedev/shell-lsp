package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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

func TestWriteAtomicLeavesTheTargetIntactOnFailure(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Windows ignores Unix permission bits, so chmod cannot make a directory read-only")
	}
	if os.Geteuid() == 0 {
		t.Skip("root writes into read-only directories")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "a.sh")
	if err := os.WriteFile(path, []byte(ugly), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if err := writeAtomic(path, []byte(pretty)); err == nil {
		t.Fatal("writing into a read-only directory succeeded")
	}
	if got := readFile(t, path); got != ugly {
		t.Errorf("a failed write changed the file to %q", got)
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
