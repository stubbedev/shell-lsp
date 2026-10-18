package hostnav

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/hadolint"
	"github.com/stubbedev/shell-lsp/internal/host/hosttest"
	"github.com/stubbedev/shell-lsp/internal/host/oracle"
)

const (
	hadolintCase    = "### "
	hadolintBoth    = ">>> "
	hadolintOurs    = ">>+ "
	hadolintTheirs  = ">>- "
	hadolintNone    = "none"
	hadolintReason  = " -- "
	hadolintConfigs = ".hadolint.yaml"
)

type hadolintExpectation struct {
	mark    string
	finding string
	reason  string
}

type hadolintFixture struct {
	name     string
	text     string
	expected []hadolintExpectation
}

func (c hadolintFixture) want(marks ...string) []string {
	out := []string{}
	for _, e := range c.expected {
		if slices.Contains(marks, e.mark) && e.finding != hadolintNone {
			out = append(out, e.finding)
		}
	}
	slices.Sort(out)
	return out
}

func parseHadolintFixtures(t *testing.T, path string) []hadolintFixture {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []hadolintFixture
	for number, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		if name, ok := strings.CutPrefix(line, hadolintCase); ok {
			out = append(out, hadolintFixture{name: name})
			continue
		}
		if len(out) == 0 {
			t.Fatalf("%s:%d: text before the first %q case", path, number+1, hadolintCase)
		}
		c := &out[len(out)-1]
		mark := ""
		for _, m := range []string{hadolintBoth, hadolintOurs, hadolintTheirs} {
			if strings.HasPrefix(line, m) {
				mark = m
			}
		}
		if mark == "" {
			if len(c.expected) > 0 {
				t.Fatalf("%s:%d: Dockerfile text after the expectations of %q", path, number+1, c.name)
			}
			c.text += line + "\n"
			continue
		}
		finding, reason, _ := strings.Cut(strings.TrimPrefix(line, mark), hadolintReason)
		c.expected = append(c.expected, hadolintExpectation{mark: mark, finding: finding, reason: reason})
	}
	seen := map[string]bool{}
	for _, c := range out {
		if seen[c.name] {
			t.Fatalf("%s: case %q appears twice", path, c.name)
		}
		seen[c.name] = true
	}
	return out
}

func hadolintFixtureFiles(t testing.TB) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "hadolint", "*.fixture"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no Hadolint fixtures: %v", err)
	}
	return paths
}

func fixtureConfig(t *testing.T, fixture string) (hadolint.Config, string) {
	t.Helper()
	path := strings.TrimSuffix(fixture, ".fixture") + hadolintConfigs
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return hadolint.Config{}, ""
	}
	if err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := hadolint.ParseConfig(abs, string(raw))
	if err != nil {
		t.Fatal(err)
	}
	return cfg, abs
}

func ourHadolintFindings(t *testing.T, text string, cfg hadolint.Config) []string {
	t.Helper()
	dir := writeTree(t, map[string]string{"Dockerfile": text})
	g := groupFor(t, newWorkspace(), filepath.Join(dir, "Dockerfile"), nil)
	docker, ok := HadolintOf(g.Self, cfg)
	if !ok {
		t.Fatal("the fixture is not read as a Dockerfile")
	}
	df, _ := g.Self.Native.Dockerfile()
	return hadolint.Keys(df, docker.Host(slices.Concat(g.Findings(), docker.Findings())))
}

func theirHadolintFindings(t *testing.T, text, config string) []string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "XDG_CONFIG_HOME=" + filepath.Join(dir, "config")}
	found, err := oracle.Hadolint(t.Context(), path, config, env)
	if err != nil {
		t.Fatal(err)
	}
	reported := make([]hadolint.Reported, len(found))
	for i, f := range found {
		reported[i] = hadolint.Reported{Code: f.Code, Line: f.Line}
	}
	return hadolint.ReportedKeys(reported)
}

func TestHadolintFixtures(t *testing.T) {
	t.Parallel()

	if *update {
		hosttest.RequireTool(t, "hadolint", hosttest.HadolintVersion)
		for _, path := range hadolintFixtureFiles(t) {
			labelHadolintFixtures(t, path)
		}
	}
	for _, path := range hadolintFixtureFiles(t) {
		cfg, _ := fixtureConfig(t, path)
		for _, c := range parseHadolintFixtures(t, path) {
			t.Run(filepath.Base(path)+"/"+c.name, func(t *testing.T) {
				t.Parallel()
				if len(c.expected) == 0 {
					t.Fatalf("the case has no expectations; label it with go test ./internal/hostnav -run TestHadolintFixtures -update")
				}
				for _, e := range c.expected {
					if e.mark != hadolintBoth && strings.TrimSpace(e.reason) == "" {
						t.Errorf("%q differs from Hadolint without a reason; add one after %q", strings.TrimSpace(e.mark)+" "+e.finding, hadolintReason)
					}
				}
				if got, want := ourHadolintFindings(t, c.text, cfg), c.want(hadolintBoth, hadolintOurs); !slices.Equal(got, want) {
					t.Errorf("findings\n got %q\nwant %q\nin:\n%s", got, want, c.text)
				}
			})
		}
	}
}

func TestHadolintFixturesAgreeWithHadolint(t *testing.T) {
	t.Parallel()

	hosttest.LiveTool(t, "hadolint", hosttest.HadolintVersion)
	for _, path := range hadolintFixtureFiles(t) {
		_, config := fixtureConfig(t, path)
		for _, c := range parseHadolintFixtures(t, path) {
			t.Run(filepath.Base(path)+"/"+c.name, func(t *testing.T) {
				t.Parallel()
				if got, want := theirHadolintFindings(t, c.text, config), c.want(hadolintBoth, hadolintTheirs); !slices.Equal(got, want) {
					t.Errorf("Hadolint reports\n %q\nbut the fixture records\n %q\nin:\n%s", got, want, c.text)
				}
			})
		}
	}
}

func labelHadolintFixtures(t *testing.T, path string) {
	t.Helper()
	cfg, config := fixtureConfig(t, path)
	cases := parseHadolintFixtures(t, path)
	var sb strings.Builder
	for _, c := range cases {
		sb.WriteString(hadolintCase + c.name + "\n" + c.text)
		if len(c.expected) > 0 {
			for _, e := range c.expected {
				line := e.mark + e.finding
				if e.reason != "" {
					line += hadolintReason + e.reason
				}
				sb.WriteString(line + "\n")
			}
			continue
		}
		theirs, ours := theirHadolintFindings(t, c.text, config), ourHadolintFindings(t, c.text, cfg)
		if len(theirs) == 0 && len(ours) == 0 {
			sb.WriteString(hadolintBoth + hadolintNone + "\n")
		}
		for _, f := range slices.Sorted(slices.Values(slices.Compact(slices.Sorted(slices.Values(slices.Concat(theirs, ours)))))) {
			mark := hadolintBoth
			switch {
			case !slices.Contains(ours, f):
				mark = hadolintTheirs
			case !slices.Contains(theirs, f):
				mark = hadolintOurs
			}
			sb.WriteString(mark + f + "\n")
		}
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

const fuzzConfig = "label-schema:\n  a: url\n  b: semver\nstrict-labels: true\ntrustedRegistries: [docker.io]\noverride:\n  info: [DL1001, DL3057]\n"

func hadolintFindingsWithin(t *testing.T, text string) {
	t.Helper()
	dir := writeTree(t, map[string]string{"Dockerfile": text})
	g := groupFor(t, newWorkspace(), filepath.Join(dir, "Dockerfile"), nil)
	cfg, err := hadolint.ParseConfig(filepath.Join(dir, hadolintConfigs), fuzzConfig)
	if err != nil {
		t.Fatal(err)
	}
	docker, ok := HadolintOf(g.Self, cfg)
	if !ok {
		t.Fatalf("%q is not read as a Dockerfile", text)
	}
	for _, f := range docker.Host(slices.Concat(g.Findings(), docker.Findings())) {
		if f.Span.Start < 0 || f.Span.Start > f.Span.End || f.Span.End > len(text) {
			t.Fatalf("%s at %v lies outside the %d bytes of %q", f.Code, f.Span, len(text), text)
		}
	}
}

func FuzzHadolintRulesOnRealisticDockerfiles(f *testing.F) {
	for _, seed := range []string{"", "\x01\x02\x03", "\x02\x01\x04\x05\x06\x07", "\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c\x0d"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, choices []byte) {
		hadolintFindingsWithin(t, hosttest.GenerateDockerfile(choices))
	})
}

func FuzzHadolintRules(f *testing.F) {
	for _, path := range hadolintFixtureFiles(f) {
		raw, err := os.ReadFile(path)
		if err != nil {
			f.Fatal(err)
		}
		for section := range strings.SplitSeq(string(raw), "\n"+hadolintCase) {
			_, text, _ := strings.Cut(section, "\n")
			f.Add(text)
		}
	}
	f.Fuzz(func(t *testing.T, text string) {
		hadolintFindingsWithin(t, text)
	})
}
