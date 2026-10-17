package makefile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/stubbedev/shell-lsp/internal/host/hosttest"
)

func sameDatabase(t *testing.T, before, after string) {
	t.Helper()
	dir := t.TempDir()
	want := hosttest.MakeDatabase(t, dir, before)
	if got := hosttest.MakeDatabase(t, dir, after); got != want {
		t.Errorf("formatting changes what make reads from %q (-before +after):\n%s", before, cmp.Diff(want, got))
	}
}

func TestFormat(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob(filepath.Join("testdata", "format", "*.mk"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no format fixtures: %v", err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Format(Parse(string(raw)))
			if err != nil {
				t.Fatal(err)
			}
			golden := strings.TrimSuffix(path, ".mk") + ".formatted"
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v; record it with go test -run TestFormat -update and review it", err)
			}
			if diff := cmp.Diff(string(want), got); diff != "" {
				t.Errorf("Format(%s) (-want +got):\n%s", path, diff)
			}
			if again, err := Format(Parse(got)); err != nil || again != got {
				t.Errorf("formatting the formatted %s changes it again (%v):\n%s", path, err, cmp.Diff(got, again))
			}
		})
	}
}

func TestFormatKeepsWhatMakeReads(t *testing.T) {
	t.Parallel()
	hosttest.LiveTool(t, "make", hosttest.MakeVersion)

	paths, err := filepath.Glob(filepath.Join("testdata", "format", "*.mk"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no format fixtures: %v", err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Format(Parse(string(raw)))
			if err != nil {
				t.Fatal(err)
			}
			sameDatabase(t, string(raw), got)
		})
	}
}

func TestFormatRefuses(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"all:\n\techo $(CC\nx\n",
		"A = 1\r\nall:\n\t@echo\r\n",
		".RECIPEPREFIX = $(P)\nall:\n\t@echo\n",
		"endif\n",
	} {
		if _, err := Format(Parse(text)); !errors.Is(err, ErrUnformattable) {
			t.Errorf("Format(%q) error = %v, want ErrUnformattable", text, err)
		}
	}
}
