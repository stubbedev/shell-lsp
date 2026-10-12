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

func FuzzProject(f *testing.F) {
	for _, seed := range []string{
		"{ src = ~/foo; }",
		"{ buildPhase = ''\n  make ${toString x} '''$x ''${y}\n''; }",
		"writeShellScript \"x\" \"echo \\\"$1\\\" ${a}\"",
		"{ text = /* bash */ ''echo ${\"''\"}''; a = \"${ { b = \"}\"; } }\"; }",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		m, _ := Project(src)
		if m.Host() != src {
			t.Fatalf("the projection maps other text than %q", src)
		}
	})
}
