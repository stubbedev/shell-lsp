package dockerfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/stubbedev/shell-lsp/internal/host/hosttest"
	"github.com/stubbedev/shell-lsp/internal/host/oracle"
)

func seeds(f *testing.F) {
	f.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "parse", "*.Dockerfile"))
	if err != nil || len(paths) == 0 {
		f.Fatalf("no seed fixtures: %v", err)
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(string(raw))
	}
	for _, c := range hosttest.Sections(f, filepath.Join("..", "..", "hostnav", "testdata", "check", "docker.txt")) {
		f.Add(c.Text)
	}
}

func FuzzAnalyze(f *testing.F) {
	seeds(f)
	f.Fuzz(func(t *testing.T, text string) {
		a := Analyze(text)
		if a.Map.Host() != text {
			t.Fatalf("the projection maps other text than %q", text)
		}
		hosttest.Bounded(t, text, a.Index)
		hosttest.Findings(a.Index, Checks, Builtins)
		for offset := range len(text) + 1 {
			spans := a.File.Enclosing(offset)
			for i, s := range spans {
				if s.Start < 0 || s.End > len(text) || i > 0 && (s.Start > spans[i-1].Start || s.End < spans[i-1].End) {
					t.Fatalf("enclosing spans at %d of %q are not nested within the text: %v", offset, text, spans)
				}
			}
			if e, ok := a.File.Expect(offset); ok && (e.Span.Start < 0 || e.Span.Start > e.Span.End || e.Span.End > len(text)) {
				t.Fatalf("completion at %d expects %v outside %q", offset, e.Span, text)
			}
		}
	})
}

func FuzzRealisticParsesAgreeWithBuildKit(f *testing.F) {
	for _, seed := range []string{"", "\x01\x02\x03", "\x02\x01\x04\x05\x06\x07", "\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c\x0d"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, choices []byte) {
		hosttest.LiveTool(t, "dockerfile-oracle", hosttest.DockerfileVersion)
		text := hosttest.GenerateDockerfile(choices)
		path := filepath.Join(t.TempDir(), "Dockerfile")
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		r, err := oracle.Dockerfile(t.Context(), path, []string{"PATH=" + os.Getenv("PATH")})
		if err != nil {
			t.Fatal(err)
		}
		want := []dumped{}
		for _, in := range r.Instructions {
			want = append(want, fromOracle(in))
		}
		raw, _ := json.Marshal(want)
		var normalized []dumped
		if err := json.Unmarshal(raw, &normalized); err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(normalized, ours(Parse(text))); diff != "" {
			t.Errorf("Parse(%q) differs from BuildKit (-buildkit +ours):\n%s", text, diff)
		}
	})
}
