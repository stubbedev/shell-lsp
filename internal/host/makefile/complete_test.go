package makefile

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
		{"line start", "|", directiveKinds, ""},
		{"partial directive", "inc|", directiveKinds, "inc"},
		{"reference", "X := $(CF|", referenceKinds, "CF"},
		{"closed reference", "X := $(CF|)", referenceKinds, "CF"},
		{"braced reference", "X := ${CF|}", referenceKinds, "CF"},
		{"function argument", "X := $(patsubst %.c,%.o,$(SR|))", referenceKinds, "SR"},
		{"function argument text", "X := $(patsubst %.c,%.o,ab|c)", nil, ""},
		{"function name", "X := $(pat|", referenceKinds, "pat"},
		{"reference in a recipe", "all:\n\techo $(CF|", referenceKinds, "CF"},
		{"recipe text", "all:\n\tec|ho", nil, ""},
		{"prerequisite", "all: bu|", targetKinds, "bu"},
		{"second prerequisite", "all: a b|", targetKinds, "b"},
		{"ifdef name", "ifdef DE|\nendif", variableKinds, "DE"},
		{"export name", "export TO|", variableKinds, "TO"},
		{"assignment value", "X := ab|", nil, ""},
		{"comment", "# ab|", nil, ""},
		{"after the reference", "X := $(CF)|", nil, ""},
	} {
		offset := strings.Index(tt.text, "|")
		text := tt.text[:offset] + tt.text[offset+1:]
		got, ok := Parse(text).Expect(offset)
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

	for _, tt := range []struct {
		kind model.Kind
		name string
		want bool
	}{
		{model.KindVariable, "CFLAGS", true},
		{model.KindVariable, "my.var-1", true},
		{model.KindVariable, "a b", false},
		{model.KindVariable, "a=b", false},
		{model.KindVariable, "a:b", false},
		{model.KindVariable, "$(x)", false},
		{model.KindVariable, "", false},
		{model.KindTarget, "build", true},
		{model.KindTarget, "out/app.o", true},
		{model.KindTarget, "%.o", false},
		{model.KindTarget, "a:b", false},
		{model.KindTarget, "a;b", false},
	} {
		if got := ValidName(tt.kind, tt.name); got != tt.want {
			t.Errorf("ValidName(%s, %q) = %v, want %v", tt.kind, tt.name, got, tt.want)
		}
	}
}
