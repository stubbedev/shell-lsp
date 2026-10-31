package nix

import (
	"slices"
	"testing"
)

func TestTokenizeKeepsEveryPathWhole(t *testing.T) {
	t.Parallel()

	src := "{ a = ~/foo; b = ./x.nix; c = ../y; d = /etc/z; e = <nixpkgs>; }"
	var paths []string
	for _, tok := range tokenize(src) {
		if tok.kind == otherToken {
			paths = append(paths, tok.text)
		}
	}
	if want := []string{"~/foo", "./x.nix", "../y", "/etc/z", "<nixpkgs>"}; !slices.Equal(paths, want) {
		t.Errorf("paths = %q, want %q", paths, want)
	}
}

func FuzzAnalyze(f *testing.F) {
	for _, seed := range []string{
		"{ src = ~/foo; }",
		"{ buildPhase = ''\n  make ${toString x} '''$x ''${y}\n''; }",
		"writeShellScript \"x\" \"echo \\\"$1\\\" ${a}\"",
		"{ text = /* bash */ ''echo ${\"''\"}''; a = \"${ { b = \"}\"; } }\"; }",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		a := Analyze(src)
		if a.Map.Host() != src {
			t.Fatalf("the projection maps other text than %q", src)
		}
		end := 0
		for _, s := range a.Index.Shells {
			if s.Span.Start < end || s.Span.End <= s.Span.Start || s.Span.End > len(src) {
				t.Fatalf("shell spans of %q overlap or are empty: %+v", src, a.Index.Shells)
			}
			end = s.Span.End
		}
		for _, sym := range a.Index.Symbols {
			if sym.Selection.Start < sym.Span.Start || sym.Selection.End > sym.Span.End || sym.Span.End > len(src) || src[sym.Selection.Start:sym.Selection.End] != sym.Name {
				t.Fatalf("symbol %+v of %q is not where it is named", sym, src)
			}
		}
	})
}
