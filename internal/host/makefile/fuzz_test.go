package makefile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func FuzzFormatKeepsWhatMakeReads(f *testing.F) {
	seeds(f)
	f.Fuzz(func(t *testing.T, text string) {
		formatted, err := Format(Parse(text))
		if err != nil {
			if !errors.Is(err, ErrUnformattable) {
				t.Fatalf("Format(%q) fails with %v, not ErrUnformattable", text, err)
			}
			return
		}
		if again, err := Format(Parse(formatted)); err != nil || again != formatted {
			t.Fatalf("formatting %q twice gives %q, then %q (%v)", text, formatted, again, err)
		}
		if !hosttest.MakeIsolated(text) || !hosttest.MakeIsolated(formatted) {
			t.Skip("make could run a command from the input")
		}
		if text != "" && !strings.HasSuffix(text, "\n") {
			t.Skip("GNU Make 4.4.1 reads a last line without a newline together with bytes the line before left in its buffer, so `XXXX=0\\n0::` fails while `X=0000000\\n0::` does not")
		}
		hosttest.LiveTool(t, "make", hosttest.MakeVersion)
		sameDatabase(t, text, formatted)
	})
}
