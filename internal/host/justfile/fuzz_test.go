package justfile

import (
	"cmp"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"

	"github.com/stubbedev/shell-lsp/internal/host/hosttest"
	"github.com/stubbedev/shell-lsp/internal/host/model"
)

func seeds(f *testing.F) {
	f.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "*", "*.just"))
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
	for _, c := range hosttest.Sections(f, filepath.Join("..", "..", "hostnav", "testdata", "check", "just.txt")) {
		f.Add(c.Text)
	}
}

type named struct {
	kind model.Kind
	name string
}

func symbolNames(ix *model.Index) []named {
	out := make([]named, len(ix.Symbols))
	for i, s := range ix.Symbols {
		out[i] = named{s.Kind, s.Name}
	}
	slices.SortFunc(out, func(a, b named) int { return cmp.Or(cmp.Compare(a.kind, b.kind), cmp.Compare(a.name, b.name)) })
	return out
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
		formatted, err := Format(a.File)
		if err != nil {
			if !errors.Is(err, ErrUnformattable) {
				t.Fatalf("Format(%q) failed without saying why: %v", text, err)
			}
			return
		}
		again := Analyze(formatted)
		if len(again.File.Errors) > 0 {
			t.Fatalf("formatting %q gives %q, which does not parse: %+v", text, formatted, again.File.Errors)
		}
		if len(again.File.Comments) != len(a.File.Comments) {
			t.Fatalf("formatting %q gives %q with %d comments instead of %d", text, formatted, len(again.File.Comments), len(a.File.Comments))
		}
		if diff := gocmp.Diff(symbolNames(a.Index), symbolNames(again.Index), gocmp.AllowUnexported(named{})); diff != "" {
			t.Fatalf("formatting %q gives %q with other symbols (-before +after):\n%s", text, formatted, diff)
		}
		if twice, err := Format(again.File); err != nil || twice != formatted {
			t.Fatalf("formatting %q twice gives %q, then %q (%v)", text, formatted, twice, err)
		}
	})
}

func FuzzMatchesJust(f *testing.F) {
	seeds(f)
	f.Fuzz(func(t *testing.T, text string) {
		hosttest.LiveTool(t, "just", hosttest.JustVersion)
		a := Analyze(text)
		if len(a.File.Errors) > 0 || !hosttest.JustVerdict(t, text).Accepted {
			return
		}
		path := filepath.Join(t.TempDir(), "justfile")
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		if diff := gocmp.Diff(any(justDumpOf(t, path)), ourDump(t, a.File)); diff != "" {
			t.Fatalf("parse of %q differs from just (-just +ours):\n%s", text, diff)
		}
		formatted, err := Format(a.File)
		if err != nil {
			return
		}
		if want := justFormat(t, text); formatted != want {
			t.Fatalf("Format(%q) differs from just --fmt:\n%s", text, gocmp.Diff(want, formatted))
		}
	})
}
