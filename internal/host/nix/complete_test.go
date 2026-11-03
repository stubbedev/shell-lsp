package nix

import (
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

func marked(t *testing.T, src string) (string, int) {
	t.Helper()
	at := strings.Index(src, "|")
	if at < 0 {
		t.Fatalf("%q has no cursor", src)
	}
	return src[:at] + src[at+1:], at
}

func completeMarked(t *testing.T, src string, files Files) []Item {
	t.Helper()
	text, at := marked(t, src)
	return Complete(text, at, files, NoEvaluation)
}

func labelsOf(items []Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Label
	}
	return out
}

func TestCompleteAtEveryGrammarPosition(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src string
		want, not []string
	}{
		{name: "an empty file", src: "|", want: []string{"map", "let", "if", "with", "assert", "rec"}, not: []string{"in", "then", "inherit"}},
		{name: "after let", src: "let |", want: []string{"in", "inherit"}, not: []string{"map", "if"}},
		{name: "a let binding before in", src: "let a = 1; | in a", want: []string{"inherit"}, not: []string{"in", "a", "map"}},
		{name: "a let binding with no in yet", src: "let a = 1; |", want: []string{"in", "inherit"}, not: []string{"a"}},
		{name: "a typed in", src: "let a = 1; in|", want: []string{"in"}},
		{name: "a let value", src: "let a = 1; b = |; in a", want: []string{"a", "b", "map", "let"}},
		{name: "past the end of a let", src: "let a = 1; in |", want: []string{"a", "map", "let", "if"}, not: []string{"inherit", "in"}},
		{name: "outside a closed let", src: "(let a = 1; in a) |", want: []string{"map", "rec"}, not: []string{"a", "let", "if"}},
		{name: "past the end of a lambda", src: "x: |", want: []string{"x", "map"}},
		{name: "a set key", src: "{ a = 1; | }", want: []string{"inherit"}, not: []string{"a", "map", "if"}},
		{name: "an empty set that may be a pattern", src: "{ | }", want: []string{"inherit"}, not: []string{"map"}},
		{name: "a recursive set value", src: "rec { a = 1; b = |; }", want: []string{"a", "b", "map"}},
		{name: "a plain set value", src: "{ a = 1; b = |; }", want: []string{"map"}, not: []string{"a", "b"}},
		{name: "a pattern formal", src: "{ a, | }: a", not: []string{"a", "map", "inherit"}},
		{name: "a closed pattern", src: "{ | }: 1", not: []string{"map", "inherit"}},
		{name: "a formal default", src: "{ a ? |, b }: a", want: []string{"a", "b", "map", "if"}},
		{name: "a lambda argument", src: "|: 1", not: []string{"map", "if"}},
		{name: "attributes of a let set", src: "let s = { one = 1; two.three = 2; }; in s.|", want: []string{"one", "two"}, not: []string{"s", "map", "inherit"}},
		{name: "nested attributes", src: "let s = { two.three = 2; }; in s.two.|", want: []string{"three"}, not: []string{"two"}},
		{name: "merged attribute definitions", src: "let s.a = 1; s.b = 2; in s.|", want: []string{"a", "b"}},
		{name: "a set literal", src: "{ one = 1; }.|", want: []string{"one"}},
		{name: "a set through another binding", src: "let s = { one = 1; }; t = s; in t.|", want: []string{"one"}},
		{name: "a has-attr test", src: "let s = { one = 1; }; in s ? |", want: []string{"one"}, not: []string{"map"}},
		{name: "a function result", src: "let f = x: { one = x; }; in (f 1).|", want: []string{"one"}},
		{name: "an argument is unknown", src: "pkgs: pkgs.|", not: []string{"pkgs", "map"}},
		{name: "a set made by an operator is unknown", src: "let s = { a = 1; } // { b = 2; }; in s.|", not: []string{"a", "b"}},
		{name: "a self reference ends", src: "let a = a; in a.|", not: []string{"a"}},
		{name: "an alias holds the formals", src: "args @ { one, ... }: args.|", want: []string{"one"}},
		{name: "builtins members", src: "builtins.|", want: []string{"map", "attrNames", "true", "builtins"}, not: []string{"__add", "__curPos", "let"}},
		{name: "builtins members refuse aliases when typed", src: "builtins.__|", want: []string{"map"}, not: []string{"__add", "__curPos"}},
		{name: "shadowed builtins", src: "let builtins = { mine = 1; }; in builtins.|", want: []string{"mine"}, not: []string{"map"}},
		{name: "an alias of builtins", src: "let b = builtins; in b.|", want: []string{"attrNames"}},
		{name: "aliases stay hidden", src: "|", not: []string{"__add", "__curPos"}},
		{name: "aliases when typed", src: "__a|", want: []string{"__add", "map"}},
		{name: "a word under the cursor", src: "ma|p", want: []string{"map"}},
		{name: "with a known set", src: "with { one = 1; }; |", want: []string{"one", "map"}},
		{name: "with builtins", src: "with builtins; |", want: []string{"attrNames"}},
		{name: "with an unknown set", src: "p: with p; |", want: []string{"p", "map"}},
		{name: "inherit from a set", src: "let s = { one = 1; }; in { inherit (s) |; }", want: []string{"one"}, not: []string{"s", "map"}},
		{name: "inherit a name", src: "let a = 1; in { inherit |; }", want: []string{"a", "map"}, not: []string{"inherit"}},
		{name: "a let inherit reads outside", src: "let a = 1; inherit |; in a", want: []string{"map"}, not: []string{"a"}},
		{name: "keys of a call", src: "let f = { one, two ? 2 }: one; in f { | }", want: []string{"one", "two", "inherit"}, not: []string{"f", "map"}},
		{name: "keys not yet passed", src: "let f = { one, two ? 2 }: one; in f { one = 1; | }", want: []string{"two"}, not: []string{"one"}},
		{name: "keys passed by inherit", src: "let f = { one, two ? 2 }: one; one = 1; in f { inherit one; | }", want: []string{"two"}, not: []string{"one"}},
		{name: "keys of a later curried argument", src: "let f = x: { one }: one; in f 1 { | }", want: []string{"one"}},
		{name: "keys in parentheses", src: "let f = { one, two }: one; in f ({ two = 2; | })", want: []string{"one"}, not: []string{"two"}},
		{name: "keys of a selected function", src: "let s = { f = { one }: one; }; in s.f { | }", want: []string{"one"}},
		{name: "keys of an unknown function", src: "g: g { | }", want: []string{"inherit"}, not: []string{"g"}},
		{name: "keys of a parenthesised set", src: "let f = { one, two ? 2 }: one; in f ({ | })", want: []string{"one", "two", "inherit"}, not: []string{"f", "map"}},
		{name: "keys of a parenthesised set with a binding", src: "let f = { one, two ? 2 }: one; in f ({ one = 1; | })", want: []string{"two"}, not: []string{"one"}},
		{name: "members through a pipe into a lambda", src: "let z = r.|; s = { a = 1; }; r = s |> (y: y); in z", want: []string{"a"}},
		{name: "members through a pipe from a lambda", src: "let z = r.|; f = y: y; s = { a = 1; }; r = f <| s; in z", want: []string{"a"}},
		{name: "keys of a piped set", src: "let f = { one, two }: one; in { one = 1; | } |> f", want: []string{"two", "inherit"}, not: []string{"one"}},

		{name: "a value inside a call set", src: "let f = { one }: one; a = 1; in f { one = |; }", want: []string{"a", "f", "map"}, not: []string{"one"}},
		{name: "then after a condition", src: "a: if a |", want: []string{"then", "a"}, not: []string{"else"}},
		{name: "then after an operator", src: "a: if a == a |", want: []string{"then"}},
		{name: "no then at a condition", src: "if |", want: []string{"map", "if"}, not: []string{"then"}},
		{name: "then already written", src: "a: if a | then 1 else 2", not: []string{"then"}},
		{name: "else after then", src: "a: if a then a |", want: []string{"else"}, not: []string{"then"}},
		{name: "else already written", src: "a: if a then a | else 2", not: []string{"else"}},
		{name: "or after a selection", src: "a: a.b |", want: []string{"or", "a"}},
		{name: "an argument takes no let", src: "f: f |", want: []string{"rec", "f"}, not: []string{"let", "if", "with", "assert"}},
		{name: "a list item takes no let", src: "[ | ]", want: []string{"rec", "map"}, not: []string{"let", "if"}},
		{name: "an operand takes no let", src: "1 + |", want: []string{"rec", "map"}, not: []string{"let", "if"}},
		{name: "a parenthesis takes anything", src: "(|)", want: []string{"let", "if"}},
		{name: "a with scope", src: "with |; 1", want: []string{"let", "map"}},
		{name: "an assert condition", src: "assert |; 1", want: []string{"if", "map"}},
		{name: "an interpolation", src: "let name = 1; in \"a${|}\"", want: []string{"name", "map", "if"}},
		{name: "an indented interpolation", src: "let name = 1; in ''\n  echo ${|}\n''", want: []string{"name", "map"}},
		{name: "a dynamic key", src: "let name = 1; in { ${|} = 1; }", want: []string{"name", "map"}},
		{name: "a comment", src: "# a |\n1", not: []string{"map", "if"}},
		{name: "a block comment", src: "/* | */ 1", not: []string{"map"}},
		{name: "a string", src: "\"a |\"", not: []string{"map"}},
		{name: "a whole string", src: "\"|\"", not: []string{"map"}},
		{name: "a path", src: "./a|", not: []string{"map"}},
		{name: "a number", src: "12|", not: []string{"map"}},
		{name: "a broken value before", src: "{ a = ; b = |", want: []string{"map", "if"}},
		{name: "a binding that never ends", src: "let a = 1; b = |\nin a", want: []string{"a", "b"}},
		{name: "a selection before an operator", src: "let x = { y = 1; }; in x.| + 1", want: []string{"y"}},
		{name: "a selection before the next binding", src: "let x = { y = 1; }; in { a = x.|\n  b = 2; }", want: []string{"y"}},
		{name: "an unclosed parenthesis after stray text", src: "f = x: (|", want: []string{"x"}},
		{name: "an unclosed list", src: "x: [ |", want: []string{"x"}},
		{name: "an unclosed call set", src: "let f = { one }: one; in f { |", want: []string{"one"}},
		{name: "a keyword typed as a binding", src: "let a = 1; inh| b = 2; in a", want: []string{"inherit"}},
		{name: "non-ASCII before the cursor", src: "let a = \"é😀\"; in |", want: []string{"a"}},
		{name: "non-ASCII comment before the cursor", src: "# é😀\nlet a = 1; in \"é😀${|}\"", want: []string{"a"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := labelsOf(completeMarked(t, tt.src, NoFiles))
			for _, want := range tt.want {
				if !slices.Contains(got, want) {
					t.Errorf("%q lacks %q in %q", tt.src, want, got)
				}
			}
			for _, not := range tt.not {
				if slices.Contains(got, not) {
					t.Errorf("%q offers %q", tt.src, not)
				}
			}
			if !slices.IsSorted(got) {
				t.Errorf("%q is not in order: %q", tt.src, got)
			}
			if len(slices.Compact(slices.Clone(got))) != len(got) {
				t.Errorf("%q has duplicates: %q", tt.src, got)
			}
		})
	}
}

func itemNamed(t *testing.T, items []Item, label string) Item {
	t.Helper()
	i := slices.IndexFunc(items, func(it Item) bool { return it.Label == label })
	if i < 0 {
		t.Fatalf("%q is not in %q", label, labelsOf(items))
	}
	return items[i]
}

func TestCompleteDescribesEachItem(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src, label string
		kind             model.Kind
		detail, doc      string
	}{
		{name: "a let value", src: "let\n  # Says one.\n  # And more.\n  one = 1;\nin |", label: "one", kind: model.KindVariable, detail: "one = 1;", doc: "Says one.\nAnd more."},
		{name: "a let function", src: "let f = x: x; in |", label: "f", kind: model.KindFunction, detail: "f = x: x;"},
		{name: "a long binding", src: "let\n  s = {\n    a = 1;\n  };\nin |", label: "s", kind: model.KindVariable, detail: "s = …;"},
		{name: "a let inherit", src: "x: let inherit x; in |", label: "x", kind: model.KindVariable, detail: "inherit x;"},
		{name: "a rec attribute", src: "rec { a = 1; b = |; }", label: "a", kind: model.KindAttribute, detail: "a = 1;"},
		{name: "a lambda argument", src: "x: |", label: "x", kind: model.KindParameter, detail: "x"},
		{name: "a formal", src: "{\n  # About b.\n  b ? 1,\n}: |", label: "b", kind: model.KindParameter, detail: "b ? 1", doc: "About b."},
		{name: "an alias", src: "a @ { b }: |", label: "a", kind: model.KindParameter, detail: "a"},
		{name: "a builtin", src: "|", label: "map", kind: model.KindFunction, detail: "map f list"},
		{name: "a constant", src: "|", label: "true", kind: model.KindVariable, detail: "true"},
		{name: "a member", src: "let s = {\n  # About f.\n  f = x: x;\n}; in s.|", label: "f", kind: model.KindFunction, detail: "f = x: x;", doc: "About f."},
		{name: "a formal through its alias", src: "a @ { b ? 1 }: a.|", label: "b", kind: model.KindAttribute, detail: "b ? 1"},
		{name: "a quoted member", src: "let s = { \"x y\" = 1; }; in s.|", label: "\"x y\"", kind: model.KindAttribute, detail: "\"x y\" = 1;"},
		{name: "a passed key", src: "let f = {\n  # About one.\n  one ? 1,\n}: one; in f { | }", label: "one", kind: model.KindAttribute, detail: "one ? 1", doc: "About one."},
		{name: "a binding the cursor is inside", src: "let a = { b = |; }; in a", label: "a", kind: model.KindVariable, detail: "a = { b = ; };"},
		{name: "a keyword", src: "|", label: "let", kind: model.KindKeyword},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			it := itemNamed(t, completeMarked(t, tt.src, NoFiles), tt.label)
			if it.Kind != tt.kind || it.Detail != tt.detail || tt.doc != "" && it.Doc != tt.doc {
				t.Errorf("item = %+v, want kind %s, detail %q, doc %q", it, tt.kind, tt.detail, tt.doc)
			}
		})
	}
	if it := itemNamed(t, completeMarked(t, "|", NoFiles), "map"); !strings.Contains(it.Doc, "`f`") {
		t.Errorf("builtin doc = %q", it.Doc)
	}
	if it := itemNamed(t, completeMarked(t, "|", NoFiles), "let"); !strings.Contains(it.Doc, "mutually recursive") {
		t.Errorf("keyword doc = %q", it.Doc)
	}
}

func TestCompleteReplacesTheWordUnderTheCursor(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src, want string
	}{
		{"nothing typed", "x: |", ""},
		{"a prefix", "x: ma|", "ma"},
		{"the middle of a word", "x: ma|p", "map"},
		{"a dash and a quote", "x: a-b'|", "a-b'"},
		{"after non-ASCII", "\"é😀${ma|}\"", "ma"},
		{"a member", "builtins.ma|", "ma"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			text, _ := marked(t, tt.src)
			items := completeMarked(t, tt.src, NoFiles)
			if len(items) == 0 {
				t.Fatal("nothing offered")
			}
			if got := text[items[0].Span.Start:items[0].Span.End]; got != tt.want {
				t.Errorf("span covers %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCompleteReadsFormalsOfOtherFiles(t *testing.T) {
	t.Parallel()

	files := func(path string) (string, bool) {
		if path == "./y.nix" {
			return "{\n  # The compiler.\n  stdenv,\n  lib ? null,\n}: 1\n", true
		}
		return "", false
	}
	for _, src := range []string{
		"callPackage ./y.nix { | }",
		"pkgs: pkgs.callPackage ./y.nix { | }",
		"import ./y.nix { | }",
		"let y = import ./y.nix; in y { | }",
	} {
		items := completeMarked(t, src, files)
		if it := itemNamed(t, items, "stdenv"); it.Kind != model.KindAttribute || it.Doc != "The compiler." {
			t.Errorf("%q: stdenv = %+v", src, it)
		}
		if it := itemNamed(t, items, "lib"); it.Detail != "lib ? null" {
			t.Errorf("%q: lib = %+v", src, it)
		}
	}
	for _, src := range []string{
		"callPackage ./y.nix { | }",
		"callPackage ./other.nix { | }",
		"callPackage ./${x}.nix { | }",
		"let import = x: x; in import ./y.nix { | }",
		"callPackageWith { | }",
	} {
		files := files
		if strings.Contains(src, "callPackage ./y.nix") {
			files = NoFiles
		}
		if got := labelsOf(completeMarked(t, src, files)); !slices.Equal(got, []string{"inherit"}) {
			t.Errorf("%q offers %q, want only inherit", src, got)
		}
	}
}

func TestItemDocAttachesLazily(t *testing.T) {
	t.Parallel()

	text, at := marked(t, "let\n  # Says one.\n  one = 1;\nin |")
	if doc, ok := ItemDoc(text, at, "one", NoFiles, NoEvaluation); !ok || doc != "Says one." {
		t.Errorf("one = %q, %v", doc, ok)
	}
	if doc, ok := ItemDoc(text, at, "map", NoFiles, NoEvaluation); !ok || !strings.Contains(doc, "`f`") {
		t.Errorf("map = %q, %v", doc, ok)
	}
	if _, ok := ItemDoc(text, at, "gone", NoFiles, NoEvaluation); ok {
		t.Error("an item that is no longer offered has a doc")
	}
}

func TestExpectAnswersThePosition(t *testing.T) {
	t.Parallel()

	src := "let a = 1; in ma"
	got, ok := Analyze(src).Expect(len(src))
	if !ok || got.Span != (syntax.Span{Start: len(src) - 2, End: len(src)}) || !slices.Contains(got.Kinds, model.KindVariable) || !slices.Contains(got.Kinds, model.KindFunction) || !slices.Contains(got.Kinds, model.KindKeyword) {
		t.Errorf("expression = %+v, %v", got, ok)
	}
	if !slices.IsSorted(got.Kinds) {
		t.Errorf("kinds are not in order: %v", got.Kinds)
	}
	key := "{ a = 1; }"
	if got, ok := Analyze(key).Expect(len("{ ")); !ok || !slices.Equal(got.Kinds, []model.Kind{model.KindKeyword}) {
		t.Errorf("set key = %+v, %v", got, ok)
	}
	if _, ok := Analyze("# comment").Expect(3); ok {
		t.Error("a comment expects something")
	}
}

func TestEveryOffsetIsAnswered(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		"",
		"{ pkgs ? import <nixpkgs> {}, lib, ... } @ args:\nlet\n  # Doc é😀\n  f = { a, b ? \"é\" }: x: a + b;\n  inherit (lib) mkIf;\nin rec {\n  c = f { a = 1; } 2;\n  d.e = builtins.map f [ 1 ];\n  g = ''\n    echo ${c} $out ''${d}\n  '';\n  h = c.d or (with args; lib.y);\n}\n",
		"let a = ; in ) { b. = ( ; [ if then else ${ '' \"",
		"f { x = g (h \"${",
		"a @ { b, c ? (d: e.",
		"{ inherit (",
		"\xff\xfe let \xff",
	} {
		analysis := Analyze(src)
		doc := Read(src)
		for at := 0; at <= len(src); at++ {
			for _, it := range Complete(src, at, NoFiles, NoEvaluation) {
				if it.Span.Start > at || it.Span.End < at || it.Span.End > len(src) {
					t.Errorf("%q at %d: %q has span %+v", src, at, it.Label, it.Span)
				}
				if strings.Contains(it.Detail, cursorName) || strings.Contains(it.Doc, cursorName) || it.Label == cursorName {
					t.Errorf("%q at %d: the cursor leaks into %+v", src, at, it)
				}
			}
			if sig, ok := SignatureAt(src, at, NoFiles); ok {
				for _, p := range sig.Params {
					if p.Start > p.End || p.End > len(sig.Label) {
						t.Errorf("%q at %d: parameter %+v of %q", src, at, p, sig.Label)
					}
				}
				if strings.Contains(sig.Label, cursorName) {
					t.Errorf("%q at %d: the cursor leaks into %q", src, at, sig.Label)
				}
			}
			doc.Hover(at, NoEvaluation)
			doc.CommentAbove(at)
			if e, ok := analysis.Expect(at); ok && (e.Span.Start > at || e.Span.End > len(src)) {
				t.Errorf("%q at %d: expect span %+v", src, at, e.Span)
			}
			analysis.Invocation(at)
		}
	}
}

func TestALocalDefinitionHidesTheLibraryFunctionOfItsName(t *testing.T) {
	t.Parallel()

	files := func(path string) (string, bool) { return "{ one }: one\n", path == "./y.nix" }
	for _, src := range []string{
		"let callPackage = _: _: { }; in callPackage ./y.nix { | }",
		"let callPackage = other; in callPackage ./y.nix { | }",
		"let lib = { callPackage = _: _: { }; }; in lib.callPackage ./y.nix { | }",
	} {
		if got := labelsOf(completeMarked(t, src, files)); slices.Contains(got, "one") {
			t.Errorf("%q offers the formals of y.nix: %q", src, got)
		}
	}
	for _, src := range []string{
		"{ callPackage }: callPackage ./y.nix { | }",
		"let callPackage = pkgs.callPackage; in callPackage ./y.nix { | }",
		"let callPackage = pkgs.newScope { }; in callPackage ./y.nix { | }",
		"pkgs: pkgs.callPackage ./y.nix { | }",
	} {
		if got := labelsOf(completeMarked(t, src, files)); !slices.Contains(got, "one") {
			t.Errorf("%q does not offer the formals of y.nix: %q", src, got)
		}
	}
}

func TestTheLibraryIsReadFromItsSource(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"/np/lib/default.nix": "let\n  lib = makeExtensible (self: let\n    callLibs = file: import file { lib = self; };\n  in {\n    modules = callLibs ./modules.nix;\n    types = callLibs ./types.nix;\n    inherit (self.modules) mkIf;\n    inherit (builtins) mapAttrs;\n  });\nin lib\n",
		"/np/lib/modules.nix": "{ lib }:\nlet\n  /**\n    Makes it conditional.\n  */\n  mkIf = condition: content: { };\nin\n{\n  inherit mkIf;\n}\n",
		"/np/lib/types.nix":   "{ lib }:\nlet\n  outer = rec {\n    types = rec {\n      # A map.\n      attrsOf = t: t;\n    };\n  };\nin\nouter // outer.types\n",
	}
	read := func(path string) (string, bool) {
		text, ok := files[path]
		return text, ok
	}
	for _, tt := range []struct {
		names []string
		file  string
		line  int
		doc   string
	}{
		{[]string{"mkIf"}, "/np/lib/modules.nix", 6, "Makes it conditional."},
		{[]string{"modules", "mkIf"}, "/np/lib/modules.nix", 6, "Makes it conditional."},
		{[]string{"types", "attrsOf"}, "/np/lib/types.nix", 6, "A map."},
	} {
		entry, ok := NewLibrary("/np", read).Lookup(tt.names)
		if !ok || entry.Declaration.File != tt.file || entry.Declaration.Line != tt.line || entry.Doc != tt.doc {
			t.Errorf("Lookup(%q) = %+v, %v", tt.names, entry, ok)
		}
	}
	if entry, ok := NewLibrary("/np", read).Lookup([]string{"mapAttrs"}); !ok || entry.Builtin != "mapAttrs" {
		t.Errorf("a builtin = %+v, %v", entry, ok)
	}
	if _, ok := NewLibrary("/np", read).Lookup([]string{"absent"}); ok {
		t.Error("an unknown name has an entry")
	}
	if got := NewLibrary("/np", read).Members(nil); !slices.Contains(got, "mkIf") || !slices.Contains(got, "types") {
		t.Errorf("Members = %q", got)
	}
}
