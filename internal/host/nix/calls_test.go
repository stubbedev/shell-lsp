package nix

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host/model"
)

func TestCallsAreOwnedByTheInnermostFunction(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src string
		want      []string
	}{
		{"a let function", "let\n  g = y: y;\n  f = x: g x;\nin\n  f 1\n", []string{"f -> g"}},
		{"an attribute of a known set", "let\n  s = { h = z: z; };\n  f = x: s.h x;\nin f\n", []string{"f -> h"}},
		{"a nested function", "let\n  g = y: y;\n  f = x: let k = y: g y; in k x;\nin f\n", []string{"f -> k", "k -> g"}},
		{"a value passed along is not a call", "let\n  g = y: y;\n  f = x: map g x;\nin f\n", nil},
		{"recursion", "let\n  f = x: f x;\nin f\n", []string{"f -> f"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ix := Analyze(tt.src).Index
			var got []string
			for _, r := range ix.Refs {
				if r.Owner != model.NoSymbol {
					got = append(got, ix.Symbols[r.Owner].Name+" -> "+r.Name)
				}
			}
			slices.Sort(got)
			if !slices.Equal(got, tt.want) {
				t.Errorf("calls = %q, want %q", got, tt.want)
			}
		})
	}
}
