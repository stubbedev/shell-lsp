package format

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host/hosttest"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

const (
	referenceTool    = "nixfmt"
	referenceVersion = "nixfmt 1.5.0"
	repositoryRoot   = "../../../.."
)

func repositoryFiles(t testing.TB) []string {
	t.Helper()
	var out []string
	for _, pattern := range []string{"flake.nix", "nix/*.nix", "tools/dockerfile-oracle/package.nix"} {
		found, err := filepath.Glob(filepath.Join(repositoryRoot, pattern))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, found...)
	}
	corpus := filepath.Join(repositoryRoot, "corpus", "nix")
	err := filepath.WalkDir(corpus, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".nix") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

func readFile(t testing.TB, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path) //nolint:gosec // G304: the path is a fixture or a repository file the test names
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestCases(t *testing.T) {
	t.Parallel()

	inputs := hosttest.Sections(t, filepath.Join("testdata", "cases.txt"))
	goldens := hosttest.Sections(t, filepath.Join("testdata", "cases.golden"))
	if len(inputs) != len(goldens) {
		t.Fatalf("%d inputs but %d goldens", len(inputs), len(goldens))
	}
	unstable := strings.Fields(readFile(t, filepath.Join("testdata", "not-idempotent.txt")))
	for i, input := range inputs {
		t.Run(input.Name, func(t *testing.T) {
			t.Parallel()
			assertFormats(t, input.Text, goldens[i].Text, !slices.Contains(unstable, input.Name))
		})
	}
}

func TestRepositoryFiles(t *testing.T) {
	t.Parallel()

	for _, path := range repositoryFiles(t) {
		rel, err := filepath.Rel(repositoryRoot, path)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(rel, func(t *testing.T) {
			t.Parallel()
			golden := filepath.Join("testdata", "repo", rel+".golden")
			assertFormats(t, readFile(t, path), readFile(t, golden), true)
		})
	}
}

func assertFormats(t *testing.T, input, golden string, stable bool) {
	t.Helper()
	got, err := Format(input)
	if err != nil {
		t.Fatalf("Format(%q): %v", input, err)
	}
	if got != golden {
		t.Errorf("Format(%q)\n got: %q\nwant: %q", input, got, golden)
	}
	if want, have := shape(t, input), shape(t, got); want != have {
		t.Errorf("formatting %q changed the tree\n got: %s\nwant: %s", input, have, want)
	}
	if !stable {
		return
	}
	again, err := Format(got)
	if err != nil {
		t.Fatalf("Format of its own output %q: %v", got, err)
	}
	if again != got {
		t.Errorf("Format is not idempotent on %q\nonce: %q\ntwice: %q", input, got, again)
	}
}

func shape(t testing.TB, src string) string {
	t.Helper()
	file := syntax.Parse(src)
	if len(file.Errors) > 0 {
		t.Fatalf("parsing %q: %v", src, file.Errors[0])
	}
	var sb strings.Builder
	var walk func(syntax.Node)
	walk = func(n syntax.Node) {
		switch n := n.(type) {
		case *syntax.Ident:
			fmt.Fprintf(&sb, "id:%s ", n.Name)
		case *syntax.Int:
			fmt.Fprintf(&sb, "int:%s ", n.Text)
		case *syntax.Float:
			fmt.Fprintf(&sb, "float:%s ", n.Text)
		case *syntax.URI, *syntax.String:
			sb.WriteString("string(")
			if s, ok := n.(*syntax.String); ok {
				for _, p := range s.Parts {
					if in, ok := p.(*syntax.Interpolation); ok {
						walk(in.Value)
					}
				}
			}
			sb.WriteString(") ")
			return
		case *syntax.Binary:
			fmt.Fprintf(&sb, "op:%s ", n.Op)
		default:
			fmt.Fprintf(&sb, "%T ", n)
		}
		first := true
		syntax.Inspect(n, func(child syntax.Node) bool {
			if first {
				first = false
				return true
			}
			walk(child)
			return false
		})
	}
	walk(file.Root)
	return sb.String()
}

func TestRefusesSyntaxErrors(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		src          string
		line, column int
	}{
		{"{ a = ; }\n", 1, 7},
		{"{\n  a = 1;\n  b =\n}\n", 4, 1},
		{"[ 1\n", 1, 1},
		{"\"é ${\n", 1, 1},
	} {
		out, err := Format(c.src)
		if err == nil {
			t.Errorf("Format(%q) = %q, want a refusal", c.src, out)
			continue
		}
		refused, ok := err.(*RefusalError) //nolint:errorlint // Format returns the refusal itself, never a wrapped one
		if !ok {
			t.Errorf("Format(%q) failed with %T: %v", c.src, err, err)
			continue
		}
		if refused.Line != c.line || refused.Column != c.column || out != "" {
			t.Errorf("Format(%q) = %q, refused at %d:%d (%v), want %d:%d", c.src, out, refused.Line, refused.Column, err, c.line, c.column)
		}
		if !strings.Contains(err.Error(), "format") {
			t.Errorf("the refusal %q does not say what was attempted", err)
		}
	}
}

func TestShellInStringsStaysVerbatim(t *testing.T) {
	t.Parallel()

	src := "{\n  buildPhase = ''\n    if   true ;then\n      echo   $out\n    fi\n  '';\n}\n"
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	if got != src {
		t.Errorf("Format changed the shell in a string: %q", got)
	}
}

func TestAgreesWithLiveNixfmt(t *testing.T) {
	hosttest.LiveTool(t, referenceTool, referenceVersion)
	for _, path := range repositoryFiles(t) {
		src := readFile(t, path)
		assertAgrees(t, path, src)
	}
	for _, input := range hosttest.Sections(t, filepath.Join("testdata", "cases.txt")) {
		assertAgrees(t, input.Name, input.Text)
	}
}

func assertAgrees(t *testing.T, name, src string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), referenceTool, "-q", "-")
	cmd.Stdin = strings.NewReader(src)
	want, err := cmd.Output()
	if err != nil {
		return
	}
	got, err := Format(src)
	if err != nil {
		t.Errorf("%s: %v", name, err)
		return
	}
	if got != string(want) {
		t.Errorf("%s differs from nixfmt\n got: %q\nwant: %q", name, got, want)
	}
}

func FuzzFormat(f *testing.F) {
	for _, c := range hosttest.Sections(f, filepath.Join("testdata", "cases.txt")) {
		f.Add(c.Text)
	}
	f.Fuzz(func(t *testing.T, src string) {
		out, err := Format(src)
		if err != nil {
			if out != "" {
				t.Errorf("Format(%q) returned both %q and %v", src, out, err)
			}
			return
		}
		if _, err := Format(out); err != nil {
			t.Errorf("Format(%q) = %q, which does not parse: %v", src, out, err)
		}
	})
}
