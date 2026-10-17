package hostnav

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host/dockerfile"
	"github.com/stubbedev/shell-lsp/internal/host/hosttest"
	"github.com/stubbedev/shell-lsp/internal/host/oracle"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

func buildkitVerdict(t *testing.T, text string) (oracle.Verdict, []string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := oracle.Dockerfile(t.Context(), path, []string{"PATH=" + os.Getenv("PATH")})
	if err != nil {
		t.Fatal(err)
	}
	var checks []string
	for _, l := range r.Checks() {
		checks = append(checks, fmt.Sprintf("buildkit %s %d", l.Rule, l.Line))
	}
	slices.Sort(checks)
	return r.Verdict(), slices.Compact(checks)
}

func agreeOnChecks(t *testing.T, text string, recorded []string, findings []lint.Finding) {
	t.Helper()
	var reported []dockerfile.BuildKitCheck
	for _, line := range recorded {
		fields := strings.Fields(line)
		n, err := strconv.Atoi(fields[2])
		if err != nil {
			t.Fatalf("golden line %q has no line number", line)
		}
		reported = append(reported, dockerfile.BuildKitCheck{Rule: fields[1], Line: n})
	}
	for _, problem := range dockerfile.Parse(text).CheckDisagreements(reported, findings) {
		t.Errorf("%s: %q", problem, text)
	}
}

func TestDockerChecksAgreeWithBuildKit(t *testing.T) {
	t.Parallel()

	path := filepath.Join("testdata", "check", "docker.txt")
	goldenPath := strings.TrimSuffix(path, ".txt") + ".golden"
	cases := hosttest.Sections(t, path)
	analyze := findingsAlone(t, "Dockerfile")
	if *update {
		hosttest.RequireTool(t, "dockerfile-oracle", hosttest.DockerfileVersion)
		var sb strings.Builder
		for _, c := range cases {
			v, checks := buildkitVerdict(t, c.Text)
			fmt.Fprintf(&sb, "-- %s --\n%s\n", c.Name, v)
			for _, line := range checks {
				sb.WriteString(line + "\n")
			}
			for _, line := range hosttest.Describe(c.Text, analyze(c.Text)) {
				sb.WriteString(line + "\n")
			}
		}
		if err := os.WriteFile(goldenPath, []byte(sb.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	recorded := map[string][]string{}
	for _, g := range hosttest.Sections(t, goldenPath) {
		recorded[g.Name] = strings.Split(strings.TrimSuffix(g.Text, "\n"), "\n")
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			lines, ok := recorded[c.Name]
			if !ok {
				t.Fatalf("%s has no section %q; record it with -update", goldenPath, c.Name)
			}
			v, ok := oracle.Parse(lines[0])
			if !ok {
				t.Fatalf("section %q starts with %q, not a verdict", c.Name, lines[0])
			}
			var checks, described []string
			for _, line := range lines[1:] {
				if strings.HasPrefix(line, "buildkit ") {
					checks = append(checks, line)
					continue
				}
				described = append(described, line)
			}
			findings := analyze(c.Text)
			if got := hosttest.Describe(c.Text, findings); !slices.Equal(got, described) {
				t.Errorf("findings\n got %q\nwant %q", got, described)
			}
			hosttest.Agree(t, c.Text, v, findings)
			if v.Accepted {
				agreeOnChecks(t, c.Text, checks, findings)
			}
		})
	}
}
