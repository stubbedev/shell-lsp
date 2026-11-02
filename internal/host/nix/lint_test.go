package nix

import (
	"io/fs"
	"slices"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func optionalOf(src string) []lint.Finding {
	return OptionalLintFindings(src, lint.CodesOf(OptionalLintCodes()...))
}

func allFindings(src string) []lint.Finding {
	return slices.Concat(findingsOf(src), optionalOf(src))
}

func ofCode(src string, findings []lint.Finding, code lint.Code) []string {
	var out []string
	for _, f := range findings {
		if f.Code == code {
			out = append(out, src[f.Span.Start:f.Span.End])
		}
	}
	return out
}

func TestStatixLints(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		code lint.Code
		hit  map[string][]string
		miss []string
	}{
		{"bool_comparison", lint.CodeNixBoolComparison, map[string][]string{
			"true == false":  {"true == false"},
			"true != false":  {"true != false"},
			"false == false": {"false == false"},
		}, []string{"x: true == x", "x: x == false", "x: x != true", "let true = 1; in true == false"}},
		{"empty_let_in", lint.CodeNixEmptyBinding, map[string][]string{
			"let in 1": {"let"},
		}, []string{"let a = 1; in a", "{ inherit; }"}},
		{"manual_inherit", lint.CodeNixSpelledInherit, map[string][]string{
			"x: { x = x; }": {"x = x;"},
		}, []string{"let a = a; in a", "rec { a = a; }", "x: { a = x; }", "x: { x.y = x; }"}},
		{"manual_inherit_from", lint.CodeNixManualInheritFrom, map[string][]string{
			"b: { a = b.a; }":        {"a = b.a;"},
			"a: { a = a.a; }":        {"a = a.a;"},
			"f: x: { a = (f x).a; }": {"a = (f x).a;"},
			"{ a = { a = 1; }.a; }":  {"a = { a = 1; }.a;"},
			"b: rec { a = b.a; }":    {"a = b.a;"},
		}, []string{"b: { a = b.c.a; }", "b: { a = b.a or 1; }", "b: { a = b.c; }", `b: { a = b."a"; }`, `b: { "a" = b.a; }`, "b: { a.c = b.c; }"}},
		{"legacy_let_syntax", lint.CodeNixLegacyLet, map[string][]string{
			"let { body = 1; }": {"let"},
		}, []string{"rec { body = 1; }.body", "let body = 1; in body"}},
		{"collapsible_let_in", lint.CodeNixCollapsibleLet, map[string][]string{
			"let a = 1; in let b = 2; in a + b": {"let"},
		}, []string{"let a = 1; in (x: let b = 2; in a + b)", "let a = 1; in let a = 2; in a", "b: let a = b; in let b = 2; in a + b", "let a = 1; in [ (let b = 2; in a + b) ]"}},
		{"eta_reduction", lint.CodeNixEtaReduction, map[string][]string{
			"f: x: f x":                             {"x: f x"},
			"map (x: toString x) [ ]":               {"x: toString x"},
			"let f = x: g (y: f y); g = z: z; in f": {"y: f y"},
		}, []string{"f: x: f x x", "x: x x", "f: y: x: f y x", "f: x: f.g x", "x: x.y x", "f: x: 1 + f x", "let f = x: f x; in f"}},
		{"empty_pattern", lint.CodeNixEmptyPattern, map[string][]string{
			"{ ... }: 1":            {"{ ... }"},
			"{ }: 1":                {"{ }"},
			"{ ... }: { foo = 1; }": {"{ ... }"},
			"{ a = { ... }: 1; }":   {"{ ... }"},
		}, []string{"{ a, ... }: a", "x@{ ... }: x", "{ ... }: { imports = [ ]; }", "{ ... }: rec { imports = [ ]; }"}},
		{"redundant_pattern_bind", lint.CodeNixRedundantPatternBind, map[string][]string{
			"x@{ ... }: x":   {"x@{ ... }"},
			"{ ... }@x: x":   {"{ ... }@x"},
			"x @ { ... }: x": {"x @ { ... }"},
		}, []string{"x@{ a, ... }: x + a", "{ ... }: 1", "x: x"}},
		{"unquoted_uri", lint.CodeNixUnquotedURI, map[string][]string{
			"{ a = https://foo.com; }": {"https://foo.com"},
			"github:a/b":               {"github:a/b"},
		}, []string{`{ a = "https://foo.com"; }`, "./a/b"}},
		{"empty_inherit", lint.CodeNixEmptyInherit, map[string][]string{
			"{ inherit; }":        {"inherit;"},
			"s: { inherit (s); }": {"inherit (s);"},
		}, []string{"s: { inherit (s) a; }", "let in 1"}},
		{"deprecated_to_path", lint.CodeNixDeprecatedToPath, map[string][]string{
			`builtins.toPath "/x"`:         {"builtins.toPath"},
			`__toPath "/x"`:                {"__toPath"},
			`with builtins; toPath "/x"`:   {"toPath"},
			`map builtins.toPath [ "/x" ]`: {"builtins.toPath"},
		}, []string{`let toPath = x: x; in toPath "/x"`, `let builtins = { toPath = x: x; }; in builtins.toPath "/x"`, `y: y.builtins.toPath "/x"`, `y: with y; toPath "/x"`}},
		{"bool_simplification", lint.CodeNixBoolSimplification, map[string][]string{
			"!(1 == 2)":  {"!(1 == 2)"},
			"! (1 == 2)": {"! (1 == 2)"},
		}, []string{"!(1 != 2)", "!(1 < 2)", "x: y: !x == y"}},
		{"useless_has_attr", lint.CodeNixUselessHasAttr, map[string][]string{
			"x: if x ? a then x.a else 1":                {"if x ? a then x.a else 1"},
			"x: if x ? a.b then x.a.b else 1":            {"if x ? a.b then x.a.b else 1"},
			"x: if x.y ? a then x.y.a else 1":            {"if x.y ? a then x.y.a else 1"},
			"x: if (x ? a) then x.a else 1":              {"if (x ? a) then x.a else 1"},
			`x: if x ? "a" then x."a" else 1`:            {`if x ? "a" then x."a" else 1`},
			"x: a: if x ? ${a} then x.${a} else 1":       {"if x ? ${a} then x.${a} else 1"},
			`x: a: if x ? "b${a}" then x."b${a}" else 1`: {`if x ? "b${a}" then x."b${a}" else 1`},
			"x: f: if (f x) ? a then (f x).a else 1":     {"if (f x) ? a then (f x).a else 1"},
		}, []string{"x: if x ? a then x.b else 1", "x: y: if x ? a then y.a else 1", "x: if x ? a then x.a or 3 else 1", "x: if !(x ? a) then 1 else x.a"}},
		{"repeated_keys", lint.CodeNixRepeatedKeys, map[string][]string{
			"{ a.a = 1; a.b = 2; a.c = 3; }":          {"a"},
			"{ a.a = 1; a.b = 2; a.c = 3; a.d = 4; }": {"a"},
			"{ a.a = 1; b = 2; a.b = 2; a.c = 3; }":   {"a"},
			"{ a.b = 1; a = { c = 2; }; a.d = 3; }":   {"a"},
			"{ x = { a.b = 1; a.c = 2; a.d = 3; }; }": {"a"},
		}, []string{"{ a.a = 1; a.b = 2; }", "let a.a = 1; a.b = 2; a.c = 3; in a", "rec { a.b = 1; a.c = 2; a.d = 3; }", `{ "a".b = 1; a.c = 2; a.d = 3; }`, "{ a = { c = 2; }; a.b = 1; a.d = 3; a.e = 4; }"}},
		{"empty_list_concat", lint.CodeNixEmptyListConcat, map[string][]string{
			"x: [ ] ++ x":         {"[ ] ++ x"},
			"x: x ++ [ ]":         {"x ++ [ ]"},
			"[ ] ++ [ ]":          {"[ ] ++ [ ]"},
			"x: y: x ++ [ ] ++ y": {"[ ] ++ y"},
		}, []string{"x: [ 1 ] ++ x", "x: y: x ++ y"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for src, want := range tt.hit {
				if got := ofCode(src, allFindings(src), tt.code); !slices.Equal(got, want) {
					t.Errorf("%s: %s reports %q, want %q", src, tt.code, got, want)
				}
			}
			for _, src := range tt.miss {
				if got := ofCode(src, allFindings(src), tt.code); len(got) != 0 {
					t.Errorf("%s: %s reports %q, want nothing", src, tt.code, got)
				}
			}
		})
	}
}

func TestDeadnixBehaviour(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		src  string
		want []string
	}{
		{"let a = 1; b = a; in 2", []string{"SL8005 a", "SL8005 b"}},
		{"let a = b; b = a; in 2", []string{"SL8005 a", "SL8005 b"}},
		{"let a = 1; f = x: a; in 2", []string{"SL8005 a", "SL8005 f", "SL8006 x"}},
		{"let a = 1; f = x: x; in f", []string{"SL8005 a"}},
		{"x: let y = x; in 1", []string{"SL8005 y"}},
		{"let inherit (a) b; a = 1; in 2", []string{"SL8005 b", "SL8005 a"}},
		{"let inherit (a) b c; a = 1; in c", []string{"SL8005 b"}},
		{"{ a, b ? a }: 1", []string{"SL8007 a", "SL8007 b"}},
		{"{ a, b ? a }: b", nil},
		{"x: { a ? x }: 1", []string{"SL8007 a"}},
		{"x@{ a }: x", []string{"SL8007 a"}},
		{"x@{ a }: a", []string{"SL8006 x"}},
		{"x@{ a }: 1", []string{"SL8006 x", "SL8007 a"}},
		{"x@{ ... }: 1", []string{"SL8006 x", "SL8021 x@{ ... }"}},
		{"_x@{ a }: 1", []string{"SL8029 _x", "SL8007 a"}},
		{"x@{ _a }: x", []string{"SL8029 _a"}},
		{"let _ = 1; in 2", []string{"SL8029 _"}},
		{"{ _ }: 1", []string{"SL8029 _"}},
		{"_: 1", nil},
		{"_x: 1", nil},
		{"let a = 1; _b = a; in 2", []string{"SL8005 a", "SL8029 _b"}},
		{"let a = 1; in rec { a = 1; b = a; }", []string{"SL8005 a"}},
		{"{ a }: a: a", []string{"SL8007 a"}},
		{"{ a }: { a = 1; }", []string{"SL8007 a"}},
		{"{ a }: b: { inherit (b) a; }", []string{"SL8007 a"}},
		{"let __sub = _: _: 1; in 2 - 3", nil},
		{"{ outputs = { self, nixpkgs }: { }; }", nil},
		{"{ outputs = { self, nixpkgs, ... }: { }; }", []string{"SL8007 self", "SL8007 nixpkgs"}},
	} {
		got := summaries(tt.src, findingsOf(tt.src))
		got = slices.DeleteFunc(got, func(s string) bool { return s == "SL8002 a" })
		if !slices.Equal(got, tt.want) {
			t.Errorf("%s reports %q, want %q", tt.src, got, tt.want)
		}
	}
}

func TestRemovingABindingNeverLeavesAReadBehind(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		"let a = 1; b = a; in 2",
		"let a = b; b = a; in 2",
		"let inherit (a) a b; in 2",
		"{ a, b ? a }: 1",
		"a: let a = 1; b = a; in 2",
	} {
		for _, f := range findingsOf(src) {
			for _, fix := range f.Fixes {
				after := textedit.Apply(src, fix.Edits)
				for _, name := range resolutions(after) {
					if !slices.Contains(resolutions(src), name) {
						t.Errorf("%s on %q gives %q, in which %s is new", f.Code, src, after, name)
					}
				}
			}
		}
	}
}

func TestUsedUnderscoreBindings(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		src  string
		want []string
	}{
		{"let _a = 1; in _a", []string{"_a"}},
		{"{ _a }: _a", []string{"_a"}},
		{"_a@{ b }: _a", []string{"_a"}},
		{"let a = 1; in a", nil},
		{"_a: _a", nil},
		{"rec { _a = 1; b = _a; }", nil},
		{"let _a = 1; in 2", nil},
		{"let __sub = _: _: 1; in 2 - 3", nil},
	} {
		if got := ofCode(tt.src, optionalOf(tt.src), lint.CodeNixUsedUnderscore); !slices.Equal(got, tt.want) {
			t.Errorf("%s reports %q, want %q", tt.src, got, tt.want)
		}
	}
}

func applyFirst(src string, findings []lint.Finding, code lint.Code) (string, lint.Fix, bool) {
	for _, f := range findings {
		if f.Code == code && len(f.Fixes) > 0 {
			return textedit.Apply(src, f.Fixes[0].Edits), f.Fixes[0], true
		}
	}
	return "", lint.Fix{}, false
}

func TestLintFixes(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		code      lint.Code
		src, want string
		safety    lint.Safety
	}{
		{lint.CodeNixBoolComparison, "true == false", "false", lint.Safe},
		{lint.CodeNixBoolComparison, "true != false", "true", lint.Safe},
		{lint.CodeNixManualInheritFrom, "b: { a = b.a; }", "b: { inherit (b) a; }", lint.Safe},
		{lint.CodeNixManualInheritFrom, "f: x: { a = (f x).a; }", "f: x: { inherit (f x) a; }", lint.Safe},
		{lint.CodeNixLegacyLet, "let { body = 1; }", "rec { body = 1; }.body", lint.Safe},
		{lint.CodeNixCollapsibleLet, "let a = 1; in let b = 2; in a + b", "let a = 1; b = 2; in a + b", lint.Safe},
		{lint.CodeNixCollapsibleLet, "let\n  a = 1;\nin\nlet\n  b = 2;\nin\na + b\n", "let\n  a = 1;\n  b = 2;\nin\na + b\n", lint.Safe},
		{lint.CodeNixEtaReduction, "f: x: f x", "f: f", lint.Review},
		{lint.CodeNixEmptyPattern, "{ ... }: 1", "_: 1", lint.Review},
		{lint.CodeNixRedundantPatternBind, "x@{ ... }: x", "x: x", lint.Review},
		{lint.CodeNixUnquotedURI, "{ a = https://foo.com; }", `{ a = "https://foo.com"; }`, lint.Safe},
		{lint.CodeNixBoolSimplification, "!(1 == 2)", "1 != 2", lint.Safe},
		{lint.CodeNixBoolSimplification, "! ( 1 == 2 )", "1 != 2", lint.Safe},
		{lint.CodeNixBoolSimplification, "!(1 == 2) && true", "1 != 2 && true", lint.Safe},
		{lint.CodeNixBoolSimplification, "!(1 == 2) == true", "(1 != 2) == true", lint.Safe},
		{lint.CodeNixBoolSimplification, "!(1 == 2) // { }", "(1 != 2) // { }", lint.Safe},
		{lint.CodeNixBoolSimplification, "!!(1 == 2)", "!(1 != 2)", lint.Safe},
		{lint.CodeNixUselessHasAttr, "x: if x ? a then x.a else 1", "x: x.a or 1", lint.Safe},
		{lint.CodeNixUselessHasAttr, "x: if x ? a then x.a else 1 + 2", "x: x.a or (1 + 2)", lint.Safe},
		{lint.CodeNixUselessHasAttr, "x: y: if x ? a then x.a else y.b", "x: y: x.a or y.b", lint.Safe},
		{lint.CodeNixUselessHasAttr, "x: y: if x ? a then x.a else y.b or 1", "x: y: x.a or (y.b or 1)", lint.Safe},
		{lint.CodeNixEmptyListConcat, "x: [ ] ++ x", "x: x", lint.Safe},
		{lint.CodeNixEmptyListConcat, "x: x ++ [ ]", "x: x", lint.Safe},
		{lint.CodeNixEmptyListConcat, "x: y: x ++ [ ] ++ y", "x: y: x ++ y", lint.Safe},
		{lint.CodeNixEmptyInherit, "s: { inherit (s); a = s; }", "s: { a = s; }", lint.Safe},
		{lint.CodeNixPkgsLib, "{ pkgs, lib }: pkgs.lib.id lib", "{ pkgs, lib }: lib.id lib", lint.Review},
	} {
		src := tt.src
		got, fix, ok := applyFirst(src, allFindings(src), tt.code)
		if !ok {
			t.Errorf("%q has no fix for %s: %+v", src, tt.code, allFindings(src))
			continue
		}
		if got != tt.want {
			t.Errorf("fixing %q gives %q, want %q", src, got, tt.want)
		}
		if fix.Safety != tt.safety {
			t.Errorf("fixing %q: safety %d, want %d", src, fix.Safety, tt.safety)
		}
		if errs := syntax.Parse(got).Errors; len(errs) != 0 {
			t.Errorf("fixing %q gives %q, which does not parse: %+v", src, got, errs)
		}
		if ofCode(got, allFindings(got), tt.code) != nil {
			t.Errorf("fixing %q leaves %s in %q", src, tt.code, got)
		}
	}
}

func resolutions(src string) []string {
	tree := syntax.Parse(src)
	table := scope.Resolve(tree)
	var out []string
	for _, u := range table.Uses() {
		binder := "free"
		switch u.Ref.Kind {
		case scope.Global:
			binder = "global"
		case scope.Dynamic:
			binder = "with"
		case scope.Definition:
			binder = "bound"
		case scope.Undefined:
			binder = "undefined"
		}
		out = append(out, u.Name+"="+binder)
	}
	slices.Sort(out)
	return out
}

func TestSafeLintFixesKeepEveryNameMeaning(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		"b: { a = b.a; }",
		"a: { a = a.a; }",
		"let { a = 1; body = a; }",
		"let a = 1; in let b = a; in a + b",
		"let a = 1; in let inherit a; in a",
		"x: y: !(x == y)",
		"x: y: if x ? a then x.a else y",
		"x: y: x ++ [ ] ++ y",
		"x: [ ] ++ x",
		"x: true == false",
		"s: let in { inherit (s); a = https://b.c; }",
	} {
		for _, f := range allFindings(src) {
			for _, fix := range f.Fixes {
				if fix.Safety != lint.Safe || f.Code == lint.CodeNixEmptyBinding || f.Code == lint.CodeNixUnusedLet || f.Code == lint.CodeNixUnusedArgument || f.Code == lint.CodeNixUnusedFormal {
					continue
				}
				after := textedit.Apply(src, fix.Edits)
				if errs := syntax.Parse(after).Errors; len(errs) != 0 {
					t.Errorf("%s on %q gives %q, which does not parse: %+v", f.Code, src, after, errs)
					continue
				}
				before := resolutions(src)
				for _, name := range resolutions(after) {
					if !slices.Contains(before, name) {
						t.Errorf("%s on %q gives %q, in which %s is new", f.Code, src, after, name)
					}
				}
			}
		}
	}
}

func TestForeignSkipComments(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src string
		want      []string
	}{
		{"above one binding", "let\n  # deadnix: skip\n  a = 1;\n  b = 2;\nin 3", []string{"SL8005 b"}},
		{"after a binding it covers the next line", "let\n  a = 1; # deadnix: skip\n  b = 2;\nin 3", []string{"SL8005 a"}},
		{"one line of let", "# deadnix: skip\nlet a = 1; b = 2; in 3", nil},
		{"only the next line", "# deadnix: skip\nlet\n  a = 1;\n  b = 2;\nin 3", []string{"SL8005 a", "SL8005 b"}},
		{"formal", "{\n  # deadnix: skip\n  a,\n  b,\n}: 1", []string{"SL8007 b"}},
		{"argument", "# deadnix: skip\nx: 1", nil},
		{"the second argument stays", "# deadnix: skip\nx:\ny: 1", []string{"SL8006 y"}},
		{"a blank line ends it", "let\n  # deadnix: skip\n\n  a = 1;\nin 3", []string{"SL8005 a"}},
		{"another comment ends it", "let\n  # deadnix: skip\n  # other\n  a = 1;\nin 3", []string{"SL8005 a"}},
		{"under another comment", "let\n  # other\n  # deadnix: skip\n  a = 1;\nin 3", nil},
		{"a block comment", "/* deadnix: skip */\nx: 1", nil},
		{"words around it", "# keep deadnix: skip for now\nx: 1", nil},
		{"case matters", "# Deadnix: skip\nx: 1", []string{"SL8006 x"}},
		{"the space matters", "# deadnix:skip\nx: 1", []string{"SL8006 x"}},
		{"not other checks", "# deadnix: skip\nlet in x: x", []string{"SL8013 let"}},
		{"underscore names too", "# deadnix: skip\nlet _a = 1; in 2", nil},
		{"statix silences statix checks", "# statix: skip\nlet in 1", nil},
		{"statix without the colon", "# statix skip\nlet in 1", nil},
		{"statix does not silence deadnix", "# statix: skip\nlet a = 1; in 2", []string{"SL8005 a"}},
		{"deadnix does not silence statix", "# deadnix: skip\nlet in 1", []string{"SL8013 let"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := summaries(tt.src, findingsOf(tt.src)); !slices.Equal(got, tt.want) {
				t.Errorf("%q reports %q, want %q", tt.src, got, tt.want)
			}
		})
	}
}

func TestOwnDirectivesSilenceTheNewChecks(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		"# shellcheck disable=SL8017\nlet { body = 1; }\n",
		"# shellcheck disable=SL8019\nf: x: f x\n",
		"# shellcheck disable=SL8026\n{ a.a = 1; a.b = 2; a.c = 3; }\n",
	} {
		if got := summaries(src, allFindings(src)); len(got) != 0 {
			t.Errorf("%q reports %q", src, got)
		}
	}
}

func TestLintChecksShareOneWalk(t *testing.T) {
	src := "let a = 1; in let b = a; in { c = a; d = b; e = (b); }\n"
	tree := syntax.Parse(src)
	table := scope.Resolve(tree)
	allocs := testing.AllocsPerRun(20, func() {
		check(tree, table, nil)
	})
	if allocs > 60 {
		t.Errorf("checking a small file allocates %.0f objects", allocs)
	}
}

func TestPitfalls(t *testing.T) {
	t.Parallel()

	flake := func(body string) string { return "{ outputs = { self }: " + body + "; }" }
	for _, tt := range []struct {
		name string
		code lint.Code
		hit  map[string][]string
		miss []string
	}{
		{"impure in a flake", lint.CodeNixImpureInFlake, map[string][]string{
			flake(`builtins.currentSystem`): {"builtins.currentSystem"},
			flake(`builtins.currentTime`):   {"builtins.currentTime"},
			flake(`builtins.getEnv "HOME"`): {"builtins.getEnv"},
			flake(`builtins.nixPath`):       {"builtins.nixPath"},
			flake(`import <nixpkgs> { }`):   {"<nixpkgs>"},
			flake(`__currentSystem`):        {"__currentSystem"},
		}, []string{
			"builtins.currentSystem",
			"import <nixpkgs> { }",
			"{ pkgs = import <nixpkgs> { }; outputs = _: 1; }",
			flake("let builtins = { currentSystem = 1; }; in builtins.currentSystem"),
			flake("builtins.storeDir"),
			flake("./a"),
			`{ description = "x"; outputs = { self }: 1; other = <a>; }`,
		}},
		{"unpinned fetch", lint.CodeNixUnpinnedFetch, map[string][]string{
			`builtins.fetchTarball "https://a.b/c"`:                      {"builtins.fetchTarball"},
			`builtins.fetchTarball { url = "https://a.b/c"; }`:           {"builtins.fetchTarball"},
			`fetchTarball https://a.b/c`:                                 {"fetchTarball"},
			`builtins.fetchurl "https://a.b/c"`:                          {"builtins.fetchurl"},
			`builtins.fetchGit { url = "https://a.b/c"; ref = "main"; }`: {"builtins.fetchGit"},
			`__fetchurl "https://a.b/c"`:                                 {"__fetchurl"},
		}, []string{
			`builtins.fetchTarball { url = "https://a.b/c"; sha256 = "x"; }`,
			`builtins.fetchurl { url = "https://a.b/c"; sha256 = "x"; }`,
			`builtins.fetchGit { url = "https://a.b/c"; rev = "x"; }`,
			`x: builtins.fetchTarball x`,
			`x: builtins.fetchTarball ({ url = "a"; } // x)`,
			`x: builtins.fetchTarball { inherit (x) url sha256; }`,
			`let fetchTarball = x: x; in fetchTarball "https://a.b/c"`,
			`p: p.fetchurl { url = "https://a.b/c"; }`,
		}},
		{"rec override", lint.CodeNixRecOverride, map[string][]string{
			"rec { a = 1; b = a; } // { a = 2; }":               {"a"},
			"(rec { a = 1; b = a; }) // { a = 2; }":             {"a"},
			"rec { a = 1; b = a; } // { c = 1; } // { a = 2; }": {"a"},
		}, []string{
			"rec { a = 1; b = a; } // { b = 2; }",
			"rec { a = 1; } // { a = 2; }",
			"{ a = 1; } // { a = 2; }",
			"x: rec { a = 1; b = a; } // x",
			"{ a = 2; } // rec { a = 1; b = a; }",
		}},
		{"module recursion", lint.CodeNixModuleRecursion, map[string][]string{
			"{ config, ... }: { config = if config.a then { } else { }; }":                     {"config.a"},
			"{ config, ... }: { config = if !config.a then { } else { }; }":                    {"!config.a"},
			"{ config, ... }: let cfg = config.x; in { config = if cfg.a then { } else { }; }": {"cfg.a"},
			"{ config, ... }: if config.a then { config.b = 1; } else { }":                     {"config.a"},
			"{ m = { config, ... }: { config = if config.a then { } else { }; }; }":            {"config.a"},
			"{ config, lib, ... }: { imports = lib.optional config.a ./x.nix; }":               {"config.a"},
			"{ config, lib, ... }: { imports = lib.optionals config.a [ ./x.nix ]; }":          {"config.a"},
			"{ config, ... }: { imports = if config.a then [ ] else [ ]; }":                    {"config.a"},
			"{ config, lib, ... }: { imports = [ ] ++ lib.optional config.a ./x.nix; }":        {"config.a"},
		}, []string{
			"{ config, lib, ... }: { config = lib.mkIf config.a { }; }",
			"{ config, ... }: { b = if config.a then 1 else 2; }",
			"{ config, ... }: { config.b = if config.a then 1 else 2; }",
			"{ config, ... }: { imports = [ { b = config.a; } ]; }",
			"{ cfg, ... }: { config = if cfg.a then { } else { }; }",
			"{ config, ... }: { config = if false then config.a else { }; }",
			"{ config, ... }: { config = if config.a or false then { } else { }; }",
			"{ config, ... }: x: { config = if config.a then { } else { }; }",
			"{ config, ... }: if config.a then { b = 1; } else { }",
			"{ config, stdenv }: if config.a then { config.b = 1; } else { }",
			"config: { config = if config.a then { } else { }; }",
		}},
		{"top-level with", lint.CodeNixTopLevelWith, map[string][]string{
			"x: with x; 1":               {"with"},
			"with { a = 1; }; a":         {"with"},
			"x: let y = x; in with y; 1": {"with"},
		}, []string{"x: { a = with x; 1; }", "x: [ (with x; 1) ]"}},
		{"store copy", lint.CodeNixStoreCopy, map[string][]string{
			`"${./.}"`:  {"${./.}"},
			`"a${./b}"`: {"${./b}"},
			`''${/a}''`: {"${/a}"},
		}, []string{`x: "${x}"`, `"${<a>}"`, "./a"}},
		{"pkgs.lib", lint.CodeNixPkgsLib, map[string][]string{
			"{ pkgs, lib }: pkgs.lib.id lib": {"pkgs.lib"},
			"{ pkgs, lib }: pkgs.lib":        {"pkgs.lib"},
		}, []string{
			"{ pkgs }: pkgs.lib.id",
			"{ pkgs }: let lib = pkgs.lib; in lib",
			"{ pkgs, lib }: pkgs.other",
			"{ x, lib }: x.lib",
		}},
		{"unused flake input", lint.CodeNixUnusedFlakeInput, map[string][]string{
			`{ inputs.a.url = "x"; outputs = { self, ... }: { }; }`:                       {"a"},
			`{ inputs = { a.url = "x"; b.url = "y"; }; outputs = { b, ... }: b; }`:        {"a"},
			`{ inputs.a.url = "x"; outputs = i: i.b; }`:                                   {"a"},
			`{ inputs.a.url = "x"; inputs.b.url = "y"; outputs = i@{ self, ... }: i.b; }`: {"a"},
		}, []string{
			`{ inputs.a.url = "x"; outputs = { a, ... }: a; }`,
			`{ inputs.a.url = "x"; outputs = i: i.a; }`,
			`{ inputs.a.url = "x"; outputs = i: f i; }`,
			`{ inputs.a.url = "x"; outputs = i@{ self, ... }: f i; }`,
			`{ inputs.a.url = "x"; outputs = { self, ... }: f self; }`,
			`{ inputs.a.url = "x"; outputs = { self, ... }: self.inputs.a; }`,
			`{ inputs.a.url = "x"; inputs.b.url = "y"; inputs.b.inputs.c.follows = "a"; outputs = { b, ... }: b; }`,
			`{ inputs.a.url = "x"; inputs.b = { url = "y"; inputs.c.follows = "a/d"; }; outputs = { b, ... }: b; }`,
			`{ inputs = "x"; outputs = { self, ... }: 1; }`,
			`{ a.b = 1; inputs.a.url = "x"; outputs = { self, ... }: 1; }`,
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for src, want := range tt.hit {
				if got := ofCode(src, allFindings(src), tt.code); !slices.Equal(got, want) {
					t.Errorf("%s: %s reports %q, want %q", src, tt.code, got, want)
				}
			}
			for _, src := range tt.miss {
				if got := ofCode(src, allFindings(src), tt.code); len(got) != 0 {
					t.Errorf("%s: %s reports %q, want nothing", src, tt.code, got)
				}
			}
		})
	}
}

func TestModuleRecursionFix(t *testing.T) {
	t.Parallel()

	src := "{ config, lib, ... }: { config = if config.a then { b = 1; } else { }; }"
	got, fix, ok := applyFirst(src, findingsOf(src), lint.CodeNixModuleRecursion)
	want := "{ config, lib, ... }: { config = lib.mkIf config.a { b = 1; }; }"
	if !ok || got != want || fix.Safety != lint.Review {
		t.Errorf("fix = %q (%+v, %v), want %q", got, fix, ok, want)
	}
	if _, _, ok := applyFirst("{ config, ... }: { config = if config.a then { } else { }; }", findingsOf("{ config, ... }: { config = if config.a then { } else { }; }"), lint.CodeNixModuleRecursion); ok {
		t.Error("offered lib.mkIf without lib in scope")
	}
	if _, _, ok := applyFirst("{ config, lib, ... }: { config = if config.a then { } else { c = 1; }; }", findingsOf("{ config, lib, ... }: { config = if config.a then { } else { c = 1; }; }"), lint.CodeNixModuleRecursion); ok {
		t.Error("offered lib.mkIf although the else branch sets something")
	}
}

const testLock = `{"root": "root", "version": 7, "nodes": {
	"root": {"inputs": {"a": "a", "b": "b", "c": "c", "d": "d", "nixpkgs": "nixpkgs"}},
	"a": {"inputs": {"nixpkgs": "nixpkgs_2"}},
	"b": {},
	"c": {"inputs": {"nixpkgs": ["nixpkgs"]}},
	"d": {"inputs": {"nixpkgs": "nixpkgs"}},
	"nixpkgs": {"locked": {"narHash": "sha256-one"}},
	"nixpkgs_2": {"locked": {"narHash": "sha256-two"}}
}}`

func lockFiles(text string) *LockFiles {
	return NewLockFiles(func(string) ([]byte, error) { return []byte(text), nil }, time.Now, time.Hour)
}

func TestFlakeLockChecks(t *testing.T) {
	t.Parallel()

	enabled := lint.CodesOf(lint.CodeNixMissingNixpkgsFollows)
	for _, tt := range []struct {
		name, src string
		want      []string
	}{
		{"override of a missing input", `{ inputs.b.inputs.gone.follows = "a"; outputs = { self, ... }: { }; }`, []string{"SL8039 gone"}},
		{"nested override", `{ inputs = { b = { inputs.gone.follows = "a"; }; }; outputs = { self, ... }: { }; }`, []string{"SL8039 gone"}},
		{"override of a present input", `{ inputs.a.inputs.nixpkgs.follows = "nixpkgs"; outputs = { self, ... }: { }; }`, nil},
		{"an input that is not locked yet", `{ inputs.z.inputs.y.follows = "a"; outputs = { self, ... }: { }; }`, nil},
		{"own nixpkgs", `{ inputs.a.url = "x"; outputs = { self, ... }: { }; }`, []string{"SL8040 a"}},
		{"followed already", `{ inputs.c.url = "x"; outputs = { self, ... }: { }; }`, nil},
		{"the same node", `{ inputs.d.url = "x"; outputs = { self, ... }: { }; }`, nil},
		{"no nixpkgs of its own", `{ inputs.b.url = "x"; outputs = { self, ... }: { }; }`, nil},
		{"a directive", "{\n  # shellcheck disable=SL8040\n  inputs.a.url = \"x\";\n  outputs = { self, ... }: { };\n}", nil},
		{"not a flake", `{ inputs.b.inputs.gone.follows = "a"; other = 1; }`, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := lockFiles(testLock).FindingsFor("/w/flake.nix", tt.src, enabled)
			if err != nil {
				t.Fatal(err)
			}
			if s := summaries(tt.src, got); !slices.Equal(s, tt.want) {
				t.Errorf("%s reports %q, want %q", tt.src, s, tt.want)
			}
		})
	}
	const missing = `{ inputs.b.inputs.gone.follows = "a"; outputs = { self, ... }: { }; }`
	if got, err := lockFiles(testLock).FindingsFor("/w/default.nix", missing, enabled); err != nil || len(got) != 0 {
		t.Errorf("a file not named flake.nix: %v, %v", got, err)
	}
	if _, err := lockFiles("nope").FindingsFor("/w/flake.nix", missing, enabled); err == nil {
		t.Error("a broken flake.lock gave no error")
	}
	gone := NewLockFiles(func(string) ([]byte, error) { return nil, fs.ErrNotExist }, time.Now, time.Hour)
	if got, err := gone.FindingsFor("/w/flake.nix", missing, enabled); err != nil || len(got) != 0 {
		t.Errorf("no flake.lock: %v, %v", got, err)
	}
	const optional = `{ inputs.a.url = "x"; outputs = { self, ... }: { }; }`
	if got, err := lockFiles(testLock).FindingsFor("/w/flake.nix", optional, lint.CodeSet{}); err != nil || len(got) != 0 {
		t.Errorf("SL8040 not turned on: %v, %v", got, err)
	}
}

func TestDefaultsLeaveOptionalChecksOff(t *testing.T) {
	t.Parallel()

	for _, code := range OptionalLintCodes() {
		info, known := lint.Describe(code)
		if !known || info.Optional == "" {
			t.Errorf("%s is listed as optional, but its entry says %+v", code, info)
		}
	}
	for _, src := range []string{"f: x: f x", "{ ... }: 1", "{ a.a = 1; a.b = 2; a.c = 3; }", "let _a = 1; in _a", "x: with x; 1", `"${./.}"`, "{ pkgs, lib }: pkgs.lib.id lib"} {
		for _, f := range findingsOf(src) {
			if info, _ := lint.Describe(f.Code); info.Optional != "" {
				t.Errorf("%s reports %s by default", src, f.Code)
			}
		}
	}
}
