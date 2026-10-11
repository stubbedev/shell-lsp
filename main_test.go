package main

import (
	"debug/elf"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

func TestReleaseBinary(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("builds the release binary")
	}
	name := "shell-lsp"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(t.TempDir(), name)
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

func TestResolvedVersion(t *testing.T) {
	t.Parallel()

	info := func(v string) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) { return &debug.BuildInfo{Main: debug.Module{Version: v}}, true }
	}
	missing := func() (*debug.BuildInfo, bool) { return nil, false }
	for _, tc := range []struct {
		stamped string
		info    func() (*debug.BuildInfo, bool)
		want    string
	}{
		{"1.2.3", info("v9.9.9"), "1.2.3"},
		{"", info("v0.1.0"), "0.1.0"},
		{"", info("(devel)"), "dev"},
		{"", missing, "dev"},
	} {
		if got := resolvedVersion(tc.stamped, tc.info); got != tc.want {
			t.Errorf("resolvedVersion(%q) = %q, want %q", tc.stamped, got, tc.want)
		}
	}
}
