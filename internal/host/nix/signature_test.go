package nix

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host/model"
)

func TestSignatureAtCalls(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src, label string
		params           []string
		active           int
		doc              string
	}{
		{name: "a builtin", src: "map |", label: "map f list", params: []string{"f", "list"}},
		{name: "a builtin's next argument", src: "map a |", label: "map f list", params: []string{"f", "list"}, active: 1},
		{name: "a parameter inside its callee's name", src: "filter |", label: "filter f list", params: []string{"f", "list"}},
		{name: "a member of builtins", src: "builtins.attrNames |", label: "builtins.attrNames set", params: []string{"set"}},
		{name: "an argument being typed", src: "map a b|", label: "map f list", params: []string{"f", "list"}, active: 1},
		{name: "before later arguments", src: "map | x", label: "map f list", params: []string{"f", "list"}},
		{name: "a bound builtin", src: "let m = builtins.map; in m a |", label: "m f list", params: []string{"f", "list"}, active: 1},
		{name: "through with", src: "with builtins; attrNames |", label: "attrNames set", params: []string{"set"}},
		{name: "curried parameters", src: "let f = a: b: a; in f |", label: "f a b", params: []string{"a", "b"}},
		{name: "the second curried parameter", src: "let f = a: b: a; in f 1 |", label: "f a b", params: []string{"a", "b"}, active: 1},
		{name: "a partial application", src: "let f = a: b: a; in (f 1) |", label: "function b", params: []string{"b"}},
		{name: "a comment above the function", src: "let\n  # Adds.\n  f = a: a;\nin f |", label: "f a", params: []string{"a"}, doc: "Adds."},
		{name: "a selected function", src: "let s = { f = a: a; }; in s.f |", label: "s.f a", params: []string{"a"}},
		{name: "fields with defaults", src: "let f = { one, two ? 2, ... }: one; in f { | }", label: "f { one, two ? 2, ... }", params: []string{"one", "two ? 2"}},
		{name: "the next unset field", src: "let f = { one, two ? 2 }: one; in f { one = 1; | }", label: "f { one, two ? 2 }", params: []string{"one", "two ? 2"}, active: 1},
		{name: "the field being valued", src: "let f = { one, two ? 2 }: one; in f { two = |; }", label: "f { one, two ? 2 }", params: []string{"one", "two ? 2"}, active: 1},
		{name: "the field being typed", src: "let f = { one, two ? 2 }: one; in f { tw| }", label: "f { one, two ? 2 }", params: []string{"one", "two ? 2"}, active: 1},
		{name: "a pattern not yet opened", src: "let f = { one }: one; in f |", label: "f { one }", params: []string{"one"}},
		{name: "a pattern then a parameter", src: "let f = { one }: b: one; in f { one = 1; } |", label: "f { one } b", params: []string{"one", "b"}, active: 1},
		{name: "a pattern without fields", src: "let f = { ... }: 1; in f |", label: "f { ... }", params: []string{"{ ... }"}},
		{name: "a long default", src: "let f = { one ? {\n  a = 1;\n} }: one; in f { | }", label: "f { one ? … }", params: []string{"one ? …"}},
		{name: "a non-ASCII default", src: "let f = { one ? \"é😀\" }: one; in f { | }", label: "f { one ? \"é😀\" }", params: []string{"one ? \"é😀\""}},
		{name: "the innermost call", src: "let f = a: a; in map (f |) []", label: "f a", params: []string{"a"}},
		{name: "the outer call around a list", src: "map f [ | ]", label: "map f list", params: []string{"f", "list"}, active: 1},
		{name: "in a broken file", src: "let f = a: b: a; in { x = f 1 |", label: "f a b", params: []string{"a", "b"}, active: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			text, at := marked(t, tt.src)
			got, ok := SignatureAt(text, at)
			if !ok {
				t.Fatalf("%q has no signature", tt.src)
			}
			var params []string
			for _, p := range got.Params {
				params = append(params, got.Label[p.Start:p.End])
			}
			if got.Label != tt.label || !slices.Equal(params, tt.params) || got.Active != tt.active || got.Doc != tt.doc && tt.doc != "" {
				t.Errorf("%q = %+v with params %q, want %q %q active %d doc %q", tt.src, got, params, tt.label, tt.params, tt.active, tt.doc)
			}
		})
	}
}

func TestSignatureWhereThereIsNone(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		"|",
		"map| a",
		"map a b |",
		"let f = a: a; in f 1 |",
		"g: g |",
		"# map |",
		"\"map |\"",
		"let f = 1; in f |",
		"builtins.nope |",
		"builtins.__add |",
		"callPackage ./y.nix { | }",
	} {
		text, at := marked(t, src)
		if got, ok := SignatureAt(text, at); ok {
			t.Errorf("%q has the signature %+v", src, got)
		}
	}
}

func TestInvocationNamesTheCall(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		src       string
		name      string
		qualifier []string
		kind      model.Kind
		active    int
	}{
		{"map a |", "map", nil, model.KindFunction, 1},
		{"builtins.map |", "map", []string{"builtins"}, model.KindFunction, 0},
		{"let f = a: a; in f |", "f", nil, model.KindFunction, 0},
		{"let f = 1; in f |", "f", nil, model.KindVariable, 0},
	} {
		text, at := marked(t, tt.src)
		got, ok := Analyze(text).Invocation(at)
		if !ok || got.Name != tt.name || !slices.Equal(got.Qualifier, tt.qualifier) || got.Kind != tt.kind || got.Active != tt.active {
			t.Errorf("%q = %+v, %v", tt.src, got, ok)
		}
	}
	if _, ok := Analyze("1").Invocation(0); ok {
		t.Error("a number is a call")
	}
}
