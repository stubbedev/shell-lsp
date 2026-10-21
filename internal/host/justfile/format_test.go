package justfile

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/stubbedev/shell-lsp/internal/host/hosttest"
)

func justFormat(t *testing.T, text string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "justfile")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "just", "--unstable", "--justfile", path, "--working-directory", dir, "--fmt")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("just --fmt: %v\n%s", err, out)
	}
	formatted, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(formatted)
}

func TestFormatAgreesWithJust(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob(filepath.Join("testdata", "format", "*.just"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no format fixtures: %v", err)
	}
	if *update {
		hosttest.RequireTool(t, "just", hosttest.JustVersion)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			golden := strings.TrimSuffix(path, ".just") + ".formatted"
			if *update {
				if err := os.WriteFile(golden, []byte(justFormat(t, string(raw))), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v; record it with go test -run TestFormatAgreesWithJust -update", err)
			}
			got, err := Format(Parse(string(raw)))
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(string(want), got); diff != "" {
				t.Errorf("Format(%s) differs from just --fmt (-just +ours):\n%s", path, diff)
			}
			again, err := Format(Parse(got))
			if err != nil || again != got {
				t.Errorf("formatting the formatted %s changes it again (%v):\n%s", path, err, cmp.Diff(got, again))
			}
		})
	}
}

func TestFormatRefusesToDropComments(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"a: # explains a\n    echo\n",
		"[private] # hidden\na:\n    echo\n",
		"x := \"1\n",
	} {
		if _, err := Format(Parse(text)); !errors.Is(err, ErrUnformattable) {
			t.Errorf("Format(%q) error = %v, want ErrUnformattable", text, err)
		}
	}
}

func TestFormatRefusesArgumentOrderThatWouldNotSettle(t *testing.T) {
	t.Parallel()

	if _, err := Format(Parse("[arg('a0',help='')]\n[arg('a', help='')]\nA a0 a:\n")); !errors.Is(err, ErrUnformattable) {
		t.Errorf("Format error = %v, want ErrUnformattable", err)
	}
	want := "[arg('a', help='x')]\n[arg('a0', help='y')]\nA a0 a:\n"
	if got, err := Format(Parse("[arg('a0', help='y')]\n[arg('a', help='x')]\nA a0 a:\n")); err != nil || got != want {
		t.Errorf("Format = %q, %v; want %q", got, err, want)
	}
}
