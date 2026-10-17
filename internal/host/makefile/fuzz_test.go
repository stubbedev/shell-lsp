package makefile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host/hosttest"
)

func seeds(f *testing.F) {
	f.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "*", "*.mk"))
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
	for _, c := range hosttest.Sections(f, filepath.Join("..", "..", "hostnav", "testdata", "check", "make.txt")) {
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
			a.File.Invocation(offset)
			if e, ok := a.File.Expect(offset); ok && (e.Span.Start < 0 || e.Span.Start > e.Span.End || e.Span.End > len(text)) {
				t.Fatalf("completion at %d expects %v outside %q", offset, e.Span, text)
			}
		}
	})
}
