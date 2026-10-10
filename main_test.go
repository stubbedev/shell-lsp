package main

import (
	"debug/elf"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReleaseBinary(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("builds the release binary")
	}
	bin := filepath.Join(t.TempDir(), "shell-lsp")
	build := exec.CommandContext(t.Context(), "go", "build", "-trimpath", "-ldflags", "-s -w -X main.version=9.9.9-test", "-o", bin, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	out, err := exec.CommandContext(t.Context(), bin, "--version").Output()
	if err != nil {
		t.Fatalf("--version: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "shell-lsp 9.9.9-test" {
		t.Errorf("--version = %q, want the ldflags-injected version", got)
	}
	if runtime.GOOS != "linux" {
		return
	}
	f, err := elf.Open(bin)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, prog := range f.Progs {
		if prog.Type == elf.PT_INTERP {
			t.Error("binary requests a dynamic loader (PT_INTERP); it must be fully static")
		}
	}
	if libs, err := f.ImportedLibraries(); err == nil && len(libs) > 0 {
		t.Errorf("binary links shared libraries %v", libs)
	}
}
