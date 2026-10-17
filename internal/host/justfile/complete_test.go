package justfile

import (
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host/model"
)

func TestExpect(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, text string
		want       []model.Kind
		word       string
	}{
		{"line start", "|", topLevelKinds, ""},
		{"partial keyword", "al|", topLevelKinds, "al"},
		{"setting name", "set sh|", []model.Kind{model.KindSetting}, "sh"},
		{"setting value", "set shell := [x|", expressionKinds, "x"},
		{"assignment value", "a := up|", expressionKinds, "up"},
		{"function call argument", "a := uppercase(ve|", expressionKinds, "ve"},
		{"alias target", "alias b := bu|", recipeKinds, "bu"},
		{"dependency", "a: bu|\n    echo", recipeKinds, "bu"},
		{"second dependency", "a: b c|", recipeKinds, "c"},
		{"qualified dependency", "a: tools::bu|", recipeKinds, "tools::bu"},
		{"dependency in parentheses", "a: (bu|", recipeKinds, "bu"},
		{"dependency argument", "a: (build ver|", expressionKinds, "ver"},
		{"parameter default", "a x=ve|:", expressionKinds, "ve"},
		{"attribute name", "[pri|", []model.Kind{model.KindAttribute}, "pri"},
		{"second attribute", "[private, gr|", []model.Kind{model.KindAttribute}, "gr"},
		{"attribute argument", "[group(na|", expressionKinds, "na"},
		{"interpolation", "a:\n    echo {{ver|}}", expressionKinds, "ver"},
		{"unclosed interpolation", "a:\n    echo {{ver|", expressionKinds, "ver"},
		{"multi-line parentheses", "a := (\n  ver|", expressionKinds, "ver"},
		{"parameter name", "a x|:", nil, ""},
		{"recipe body", "a:\n    ec|ho", nil, ""},
		{"comment", "# ab|", nil, ""},
		{"inside a string", "a := 'x|", nil, ""},
		{"import path", "import 'x|'", nil, ""},
	} {
		offset := strings.Index(tt.text, "|")
		text := tt.text[:offset] + tt.text[offset+1:]
		f := Parse(text)
		got, ok := f.Expect(offset)
		if !ok {
			if tt.want != nil {
				t.Errorf("%s: no completion context, want %v", tt.name, tt.want)
			}
			continue
		}
		if tt.want == nil {
			t.Errorf("%s: completion context %v, want none", tt.name, got.Kinds)
			continue
		}
		if !slices.Equal(got.Kinds, tt.want) || text[got.Span.Start:got.Span.End] != tt.word {
			t.Errorf("%s: Expect = %v over %q, want %v over %q", tt.name, got.Kinds, text[got.Span.Start:got.Span.End], tt.want, tt.word)
		}
	}
}

func TestValidName(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]bool{"build": true, "build-all": true, "_x": true, "a1": true, "": false, "1a": false, "a b": false, "a::b": false, "-a": false} {
		if got := ValidName(model.KindRecipe, name); got != want {
			t.Errorf("ValidName(%q) = %v, want %v", name, got, want)
		}
	}
}
