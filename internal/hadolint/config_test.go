package hadolint

import (
	"flag"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/host/hosttest"
	"github.com/stubbedev/shell-lsp/internal/host/oracle"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

var update = flag.Bool("update", false, "record Hadolint's verdict on every configuration fixture")

const (
	verdictsFile = "testdata/configs/verdicts.golden"
	accepted     = "accepted"
	rejected     = "rejected"
)

var stricter = map[string]string{
	"disable-ignore-pragma-camelcase-ignored": "an unknown setting is an error, so a misspelt one cannot pass unnoticed",
	"failure-threshold-camelcase-ignored":     "an unknown setting is an error, so a misspelt one cannot pass unnoticed",
	"label-schema-camelcase-ignored":          "an unknown setting is an error, so a misspelt one cannot pass unnoticed",
	"no-fail-camelcase-ignored":               "an unknown setting is an error, so a misspelt one cannot pass unnoticed",
	"strict-labels-camelcase-ignored":         "an unknown setting is an error, so a misspelt one cannot pass unnoticed",
	"trusted-registries-kebab-ignored":        "an unknown setting is an error, so a misspelt one cannot pass unnoticed",
	"unknown-keys-ignored":                    "an unknown setting is an error, so a misspelt one cannot pass unnoticed",
	"override-capitalised-level-ignored":      "an unknown level is an error, so a misspelt one cannot pass unnoticed",
	"override-unknown-level-ignored":          "an unknown level is an error, so a misspelt one cannot pass unnoticed",
	"ignored-unknown-code-accepted":           "a code that is no check code is an error",
	"anchors-and-aliases":                     "the YAML reader takes no anchors or aliases",
}

var laxer = map[string]string{
	"empty-file-rejected":           "an empty configuration configures nothing",
	"comment-only-rejected":         "a configuration of comments configures nothing",
	"document-marker-only-rejected": "an empty document configures nothing",
	"null-document-rejected":        "a null document configures nothing",
}

func configFixtures(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "configs", "*.yaml"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no configuration fixtures: %v", err)
	}
	return paths
}

func fixtureName(path string) string { return strings.TrimSuffix(filepath.Base(path), ".yaml") }

func TestConfigVerdictsAgreeWithHadolint(t *testing.T) {
	t.Parallel()

	paths := configFixtures(t)
	if *update {
		hosttest.RequireTool(t, "hadolint", hosttest.HadolintVersion)
		var sb strings.Builder
		for _, path := range paths {
			abs, err := filepath.Abs(path)
			if err != nil {
				t.Fatal(err)
			}
			ok, err := oracle.HadolintAccepts(t.Context(), abs, []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			verdict := rejected
			if ok {
				verdict = accepted
			}
			sb.WriteString(fixtureName(path) + " " + verdict + "\n")
		}
		if err := os.WriteFile(verdictsFile, []byte(sb.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(verdictsFile)
	if err != nil {
		t.Fatalf("%v; record it with go test ./internal/hadolint -update", err)
	}
	verdicts := map[string]string{}
	for line := range strings.Lines(string(raw)) {
		name, verdict, _ := strings.Cut(strings.TrimSpace(line), " ")
		verdicts[name] = verdict
	}
	for _, path := range paths {
		name := fixtureName(path)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			text, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			_, err = ParseConfig(path, string(text))
			ours := map[bool]string{true: accepted, false: rejected}[err == nil]
			theirs, ok := verdicts[name]
			if !ok {
				t.Fatalf("%s has no verdict; record it with -update", name)
			}
			want := theirs
			if _, differs := stricter[name]; differs && theirs == accepted {
				want = rejected
			}
			if _, differs := laxer[name]; differs && theirs == rejected {
				want = accepted
			}
			if ours != want {
				t.Errorf("ParseConfig %s it (err %v), Hadolint %s it, and the fixture expects %s", ours, err, theirs, want)
			}
		})
	}
}

func TestConfigDifferencesHaveReasons(t *testing.T) {
	t.Parallel()

	for _, differences := range []map[string]string{stricter, laxer} {
		for _, name := range slices.Sorted(maps.Keys(differences)) {
			if _, err := os.Stat(filepath.Join("testdata", "configs", name+".yaml")); err != nil {
				t.Errorf("the difference %q names no fixture: %v", name, err)
			}
			if differences[name] == "" {
				t.Errorf("the difference %q has no reason", name)
			}
		}
	}
}

func code(t *testing.T, text string) lint.Code {
	t.Helper()
	c, err := lint.ParseCode(text)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestParseConfigReadsEverySetting(t *testing.T) {
	t.Parallel()

	cfg, err := ParseConfig("/p/.hadolint.yaml", `
ignored: [DL3008, SC2086]
override:
  error: [DL3001]
  style: [DL3001, DL3059]
  info:
trustedRegistries:
  - docker.io
  - "*.gcr.io"
label-schema:
  org.opencontainers.image.source: url
  maintainer: text
strict-labels: true
disable-ignore-pragma: false
failure-threshold: warning
no-fail: true
format: json
`)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.Ignored, []lint.Code{code(t, "DL3008"), code(t, "SC2086")}) {
		t.Errorf("Ignored = %v", cfg.Ignored)
	}
	if cfg.Overrides[code(t, "DL3001")] != lint.SeverityError || cfg.Overrides[code(t, "DL3059")] != lint.SeverityStyle {
		t.Errorf("Overrides = %v; the most severe level should win", cfg.Overrides)
	}
	if !slices.Equal(cfg.TrustedRegistries, []string{"docker.io", "*.gcr.io"}) {
		t.Errorf("TrustedRegistries = %v", cfg.TrustedRegistries)
	}
	if !slices.Equal(cfg.LabelSchema, []Label{{Name: "org.opencontainers.image.source", Format: LabelURL}, {Name: "maintainer", Format: LabelText}}) {
		t.Errorf("LabelSchema = %v", cfg.LabelSchema)
	}
	if !cfg.StrictLabels || cfg.DisableIgnorePragma || cfg.Path != "/p/.hadolint.yaml" {
		t.Errorf("StrictLabels %v, DisableIgnorePragma %v, Path %q", cfg.StrictLabels, cfg.DisableIgnorePragma, cfg.Path)
	}
}

func TestParseConfigNamesTheProblem(t *testing.T) {
	t.Parallel()

	for text, want := range map[string]string{
		"ignored: DL3008\n":                "line 1, column 10: expected a list",
		"ignored: [DL3008, XX1]\n":         "line 1, column 19: expected a check code",
		"override:\n  fatal: [DL3008]\n":   "line 2, column 3: unknown level \"fatal\"",
		"strict-labels: yes\n":             "line 1, column 16: expected true or false",
		"label-schema:\n  a: link\n":       "line 2, column 6: unknown label format \"link\"",
		"trustedregistries: [docker.io]\n": "line 1, column 1: unknown setting \"trustedregistries\"",
		"- DL3008\n":                       "line 1, column 1: the configuration is a sequence",
		"failure-threshold: fatal\n":       "line 1, column 20: \"fatal\" is none of",
	} {
		_, err := ParseConfig("/p/.hadolint.yaml", text)
		if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "/p/.hadolint.yaml") {
			t.Errorf("ParseConfig(%q) = %v, want an error naming the file and %q", text, err, want)
		}
	}
}

func TestResolverFindsTheNearestConfiguration(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	home := filepath.Join(root, "home")
	write := func(path, text string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "repo", ".hadolint.yml"), "ignored: [DL3008]\n")
	write(filepath.Join(root, "repo", "svc", ".hadolint.yaml"), "ignored: [DL3009]\n")
	write(filepath.Join(home, ".config", "hadolint.yaml"), "ignored: [DL3010]\n")
	r := NewResolver(os.ReadFile, func() time.Time { return time.Time{} }, time.Hour, ConfigFallback(home, ""))
	for dir, want := range map[string]string{
		filepath.Join(root, "repo", "svc", "deep"): "DL3009",
		filepath.Join(root, "repo", "other"):       "DL3008",
		filepath.Join(root, "elsewhere"):           "DL3010",
	} {
		cfg, found, err := r.Nearest(dir)
		if err != nil || !found || len(cfg.Ignored) != 1 || cfg.Ignored[0].String() != want {
			t.Errorf("Nearest(%s) = %v, %v, %v; want the configuration that ignores %s", dir, cfg.Ignored, found, err, want)
		}
	}
}
