package nix

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host/model"
)

func TestParameterHints(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src string
		want      []string
	}{
		{"curried", "let f = a: b: a; in f 1 2", []string{"1 a", "2 b"}},
		{"a pattern has no positional name", "let f = { a }: b: b; in f { a = 1; } 2", []string{"2 b"}},
		{"an argument spelled like its parameter", "b: let f = b: b; in f b", nil},
		{"more arguments than parameters", "let f = a: a; in f 1 2", []string{"1 a"}},
		{"a function of a set", "let s = { t.f = a: a; }; in s.t.f 1", []string{"1 a"}},
		{"a lambda called in place", "(a: a) 1", []string{"1 a"}},
		{"a parameter can be anything", "f: f 1", nil},
		{"through with is not proven", "with lib; f 1", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := Read(tt.src)
			var got []string
			for _, h := range d.ParameterHints(model.Span{End: len(tt.src)}) {
				got = append(got, tt.src[h.Offset:h.Offset+1]+" "+h.Name)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("hints = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParameterHintsStayWithinTheRange(t *testing.T) {
	t.Parallel()

	src := "let f = a: a; in [ (f 1) (f 2) ]"
	got := Read(src).ParameterHints(model.Span{Start: 25, End: len(src)})
	if len(got) != 1 || src[got[0].Offset] != '2' {
		t.Errorf("hints = %+v", got)
	}
}

func TestPathLiterals(t *testing.T) {
	t.Parallel()

	got := Read("{ imports = [ ./g ]; l = [ (import ./a) (pkgs.callPackage ./b { }) ./c ~/d /e ./f/${x} <n> ]; }").PathLiterals()
	type literal struct {
		path   string
		loaded bool
	}
	var have []literal
	for _, p := range got {
		have = append(have, literal{p.Path, p.Loaded})
	}
	want := []literal{{"./g", true}, {"./a", true}, {"./b", true}, {"./c", false}, {"/e", false}}
	if !slices.Equal(have, want) {
		t.Errorf("path literals = %+v, want %+v", have, want)
	}
}

func TestProseMarksAURLCutByAnAntiquotation(t *testing.T) {
	t.Parallel()

	src := "# c\n{ a = \"x${y}z\"; b = https://example.com; }"
	var got []string
	for _, run := range Read(src).Prose() {
		mark := ""
		if run.Continues {
			mark = "+"
		}
		got = append(got, src[run.Span.Start:run.Span.End]+mark)
	}
	if want := []string{"# c", "x+", "z", "https://example.com"}; !slices.Equal(got, want) {
		t.Errorf("prose = %q, want %q", got, want)
	}
}
