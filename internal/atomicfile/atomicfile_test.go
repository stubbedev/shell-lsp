package atomicfile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func read(t *testing.T, path string) string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestWriteCreatesAFileAndLeavesNothingBehind(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")
	if err := Write(path, []byte("fresh\n")); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != "fresh\n" {
		t.Errorf("wrote %q", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("left files behind: %v", entries)
	}
}

func TestWriteKeepsTheModeOfTheFileItReplaces(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix permission bits")
	}
	path := filepath.Join(t.TempDir(), "run.sh")
	if err := os.WriteFile(path, []byte("old\n"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("new\n")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o750 {
		t.Errorf("mode %o, want 750", got)
	}
	if got := read(t, path); got != "new\n" {
		t.Errorf("wrote %q", got)
	}
}

func TestWriteLeavesTheTargetIntactOnFailure(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Windows ignores Unix permission bits, so chmod cannot make a directory read-only")
	}
	if os.Geteuid() == 0 {
		t.Skip("root writes into read-only directories")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "a.sh")
	if err := os.WriteFile(path, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if err := Write(path, []byte("after\n")); err == nil {
		t.Fatal("writing into a read-only directory succeeded")
	}
	if got := read(t, path); got != "before\n" {
		t.Errorf("a failed write changed the file to %q", got)
	}
}
