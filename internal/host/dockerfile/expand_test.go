package dockerfile

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host/hosttest"
	"github.com/stubbedev/shell-lsp/internal/host/oracle"
)

var relativeWorkdir = regexp.MustCompile(`^Relative workdir (".*") can have`)

func buildkitWorkdir(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Dockerfile")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := oracle.Dockerfile(t.Context(), path, []string{"PATH=" + os.Getenv("PATH")})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != "" {
		return "error"
	}
	for _, l := range r.Lint {
		if m := relativeWorkdir.FindStringSubmatch(l.Message); m != nil {
			value, err := strconv.Unquote(m[1])
			if err != nil {
				t.Fatalf("BuildKit quotes the workdir as %s: %v", m[1], err)
			}
			return strconv.Quote(value)
		}
	}
	return "absolute"
}

func lastWorkdir(t *testing.T, f *File) (string, bool) {
	t.Helper()
	stages := f.Stages()
	s := stages[len(stages)-1]
	for _, in := range slices.Backward(s.Instructions) {
		if in.Name == "WORKDIR" {
			return f.evaluate(in.Args[0].Text, f.stageEnvironment(s, stages, in))
		}
	}
	t.Fatalf("the last stage of %q has no WORKDIR", f.Text)
	return "", false
}

func TestEvaluateAgreesWithBuildKit(t *testing.T) {
	t.Parallel()

	path := filepath.Join("testdata", "expand.txt")
	golden := strings.TrimSuffix(path, ".txt") + ".golden"
	cases := hosttest.Sections(t, path)
	if *update {
		hosttest.RequireTool(t, "dockerfile-oracle", hosttest.DockerfileVersion)
		var sb strings.Builder
		for _, c := range cases {
			fmt.Fprintf(&sb, "-- %s --\n%s\n", c.Name, buildkitWorkdir(t, c.Text))
		}
		if err := os.WriteFile(golden, []byte(sb.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	recorded := map[string]string{}
	for _, g := range hosttest.Sections(t, golden) {
		recorded[g.Name] = strings.TrimSuffix(g.Text, "\n")
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			want, ok := recorded[c.Name]
			if !ok {
				t.Fatalf("%s has no section %q; record it with -update", golden, c.Name)
			}
			got, known := lastWorkdir(t, Parse(c.Text))
			switch {
			case strings.HasPrefix(c.Name, "unknown-"):
				if known {
					t.Errorf("evaluate claims to know %q, BuildKit gives %s", got, want)
				}
			case !known:
				t.Errorf("evaluate does not know the value, BuildKit gives %s", want)
			case strconv.Quote(got) != want:
				t.Errorf("evaluate gives %q, BuildKit gives %s", got, want)
			}
		})
	}
}
