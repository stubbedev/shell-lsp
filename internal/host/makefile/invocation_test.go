package makefile

import (
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host/model"
)

func TestInvocation(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, text string
		want       model.Invocation
		ok         bool
	}{
		{"first argument", "x := $(subst |", model.Invocation{Name: "subst", Kind: model.KindFunction}, true},
		{"third argument", "x := $(subst a,b,|", model.Invocation{Name: "subst", Kind: model.KindFunction, Active: 2}, true},
		{"nested reference", "x := $(subst a,$(Y),|", model.Invocation{Name: "subst", Kind: model.KindFunction, Active: 2}, true},
		{"nested call", "x := $(subst a,$(strip |", model.Invocation{Name: "strip", Kind: model.KindFunction}, true},
		{"parentheses in an argument", "x := $(subst (a,b),c,|", model.Invocation{Name: "subst", Kind: model.KindFunction, Active: 2}, true},
		{"braces", "x := ${patsubst %.c,|", model.Invocation{Name: "patsubst", Kind: model.KindFunction, Active: 1}, true},
		{"recipe", "all:\n\techo $(words $(X),|", model.Invocation{Name: "words", Kind: model.KindFunction, Active: 1}, true},
		{"continued line", "x := $(subst a,\\\n  b,|", model.Invocation{Name: "subst", Kind: model.KindFunction, Active: 2}, true},
		{"variable", "x := $(FOO|", model.Invocation{}, false},
		{"closed", "x := $(strip a) |", model.Invocation{}, false},
	} {
		offset := strings.Index(tt.text, "|")
		f := Parse(tt.text[:offset] + tt.text[offset+1:])
		got, ok := f.Invocation(offset)
		if ok != tt.ok || got.Name != tt.want.Name || got.Kind != tt.want.Kind || got.Active != tt.want.Active {
			t.Errorf("%s: Invocation = %+v, %v, want %+v, %v", tt.name, got, ok, tt.want, tt.ok)
		}
	}
}
