package justfile

import (
	"slices"
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
		{"first argument", "x := replace(|", model.Invocation{Name: "replace", Kind: model.KindFunction}, true},
		{"third argument", `x := replace("a", "b", |`, model.Invocation{Name: "replace", Kind: model.KindFunction, Active: 2}, true},
		{"nested call", `x := replace(uppercase(|), "b")`, model.Invocation{Name: "uppercase", Kind: model.KindFunction}, true},
		{"after a nested call", `x := replace(uppercase("a"), |`, model.Invocation{Name: "replace", Kind: model.KindFunction, Active: 1}, true},
		{"grouped argument", `x := replace(("a" + "b"), |`, model.Invocation{Name: "replace", Kind: model.KindFunction, Active: 1}, true},
		{"interpolation", "a:\n    echo {{ env(\"A\", |) }}", model.Invocation{Name: "env", Kind: model.KindFunction, Active: 1, Scope: 1}, true},
		{"dependency", "a: (build |", model.Invocation{Name: "build", Kind: model.KindRecipe, Scope: 1}, true},
		{"second dependency argument", `a: (build "x" |`, model.Invocation{Name: "build", Kind: model.KindRecipe, Active: 1, Scope: 1}, true},
		{"typing a dependency argument", `a: (build "x" ver|`, model.Invocation{Name: "build", Kind: model.KindRecipe, Active: 1, Scope: 1}, true},
		{"concatenated dependency argument", `a: (build "x" + |`, model.Invocation{Name: "build", Kind: model.KindRecipe, Scope: 1}, true},
		{"qualified dependency", "a: (tools::lint |", model.Invocation{Name: "lint", Qualifier: []string{"tools"}, Kind: model.KindRecipe, Scope: 1}, true},
		{"inside a string argument", `x := replace("a", "b|c")`, model.Invocation{Name: "replace", Kind: model.KindFunction, Active: 1}, true},
		{"inside a dependency string", `a: (build "x" "y|")`, model.Invocation{Name: "build", Kind: model.KindRecipe, Active: 1, Scope: 1}, true},
		{"attribute", "[group(|", model.Invocation{Name: "group", Kind: model.KindAttribute}, true},
		{"after the call", `x := uppercase("a") |`, model.Invocation{}, false},
		{"plain group", "x := (|", model.Invocation{}, false},
		{"recipe body", "a:\n    echo (|", model.Invocation{}, false},
	} {
		offset := strings.Index(tt.text, "|")
		f := Parse(tt.text[:offset] + tt.text[offset+1:])
		got, ok := f.Invocation(offset)
		if ok != tt.ok || got.Name != tt.want.Name || got.Kind != tt.want.Kind || got.Active != tt.want.Active || !slices.Equal(got.Qualifier, tt.want.Qualifier) {
			t.Errorf("%s: Invocation = %+v, %v, want %+v, %v", tt.name, got, ok, tt.want, tt.ok)
		}
	}
}
