package nix

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func findingsOf(src string) []lint.Finding { return Analyze(src).Index.Findings }

func summaries(src string, findings []lint.Finding) []string {
	var out []string
	for _, f := range findings {
		out = append(out, f.Code.String()+" "+src[f.Span.Start:f.Span.End])
	}
	return out
}

func TestChecks(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		src  string
		want []string
	}{
		{`x`, []string{"SL8002 x"}},
		{`x: x`, nil},
		{`with a; b`, []string{"SL8002 a"}},
		{`y: with y; b`, nil},
		{`map`, nil},
		{`__attrNames`, nil},
		{`__map`, []string{"SL8002 __map"}},
		{`let inherit x; in x`, []string{"SL8002 x"}},
		{`rec { a.b = 1; c = a; d = b; }`, []string{"SL8002 b"}},

		{`{ a = 1; a = 2; }`, []string{"SL8003 a"}},
		{`{ a.b = 1; a.c = 2; }`, nil},
		{`{ a = { b = 1; }; a = { c = 2; }; }`, nil},
		{`{ a.b = 1; a.b = 2; }`, []string{"SL8003 b"}},
		{`let a = 1; a = 2; in a`, []string{"SL8003 a"}},

		{`{ a, a }: a`, []string{"SL8004 a"}},
		{`a@{ a }: a`, []string{"SL8004 a"}},
		{`{ a }@a: a`, []string{"SL8004 a"}},
		{`{ a, b }: a + b`, nil},

		{`let a = 1; in 2`, []string{"SL8005 a"}},
		{`let a = 1; in a`, nil},
		{`let _a = 1; in 2`, nil},
		{`let f = f; in 2`, []string{"SL8005 f"}},
		{`let f = x: f x; in 2`, []string{"SL8005 f"}},
		{`let a.b = 1; a.c = 2; in 3`, []string{"SL8005 a"}},
		{`x: let inherit (x) a b; in a`, []string{"SL8005 b"}},
		{`let a = 1; b = a; in 2`, []string{"SL8005 b"}},
		{`let inherit (a) a b; in b`, nil},
		{`let __sub = 1; in 2 - 3`, nil},
		{`let { a = 1; body = 2; }`, []string{"SL8005 a"}},
		{`let __curPos = 1; in __curPos`, []string{"SL8005 __curPos"}},
		{`rec { a = 1; b = a; }`, nil},
		{`{ a = 1; }`, nil},

		{`x: 1`, []string{"SL8006 x"}},
		{`_x: 1`, nil},
		{`_: 1`, nil},
		{`x: x`, nil},
		{`a@{ }: 1`, []string{"SL8006 a"}},
		{`{ }@a: 1`, []string{"SL8006 a"}},
		{`a@{ b }: a`, nil},
		{`{ b }@a: a`, nil},
		{`_a@{ b }: 1`, []string{"SL8007 b"}},
		{`a@{ b, ... }: b`, []string{"SL8006 a"}},

		{`{ a, b }: a`, []string{"SL8007 b"}},
		{`{ a, _b }: a`, nil},
		{`{ a, ... }: 1`, []string{"SL8007 a"}},
		{`{ ... }: 1`, nil},
		{`{ a, b ? a }: b`, nil},
		{`{ }: 1`, nil},
		{`{ __sub }: 1 - 2`, nil},
		{`{ outputs = { self, nixpkgs }: { }; }`, nil},
		{`{ outputs = { self, ... }: { }; }`, []string{"SL8007 self"}},
		{`{ other = { self }: { }; }`, []string{"SL8007 self"}},

		{`{ a = (1); }`, []string{"SL8008 (1)"}},
		{`f: f (1)`, []string{"SL8008 (1)"}},
		{`f: f(1)`, nil},
		{`f: g: f (g 1)`, nil},
		{`a: b: a /(b)`, nil},
		{`a: b: a / (b)`, []string{"SL8008 (b)"}},
		{`a: (a)-1`, nil},
		{`x: x.y or (1)`, []string{"SL8008 (1)"}},
		{`x: (x).y`, nil},
		{`x: (x.y) .z or 1`, nil},
		{`s: { inherit (s) a; }`, nil},
		{`s: { inherit ((s)) a; }`, []string{"SL8008 (s)"}},
		{`x: (x x)`, []string{"SL8008 (x x)"}},
		{`x: 1 + (x * 2)`, nil},
		{`x: [ (x x) ]`, nil},
		{`x: [ (x.y) ]`, []string{"SL8008 (x.y)"}},
		{`x: [ (x.y or 1) ]`, nil},
		{`if (true) then 1 else 2`, []string{"SL8008 (true)"}},
		{`((1))`, []string{"SL8008 ((1))", "SL8008 (1)"}},
		{`"${(1)}"`, []string{"SL8008 (1)"}},
		{`let a = 1; in (a)`, []string{"SL8008 (a)"}},
		{`x: with (x); 1`, []string{"SL8008 (x)"}},
		{`x: assert (x); 1`, []string{"SL8008 (x)"}},
		{`{ a ? (1) }: a`, []string{"SL8008 (1)"}},
		{`let or = 1; in (x: x) or`, []string{"SL8012 or"}},

		{`x: if x then true else false`, []string{"SL8009 if x then true else false"}},
		{`x: if x then false else true`, []string{"SL8009 if x then false else true"}},
		{`true: x: if x then true else false`, nil},
		{`x: if x then 1 else false`, nil},
		{`x: if x then true else true`, nil},

		{`rec { a = 1; }`, []string{"SL8010 rec"}},
		{`rec { }`, []string{"SL8010 rec"}},
		{`rec { a = 1; b = a; }`, nil},
		{`rec { __overrides = { }; a = 1; }`, nil},
		{`x: rec { inherit x; }`, []string{"SL8010 rec"}},
		{`x: rec { inherit (x) a; b = a; }`, nil},
		{`rec { a = 1; b = with a; a; }`, nil},
		{`{ a = rec { b = 1; }; a = { c = b; }; }`, nil},
		{`{ a = { b = 1; }; a = rec { c = 2; }; }`, []string{"SL8010 rec"}},
		{`{ a = rec { b = 1; }; a = rec { c = b; }; }`, []string{"SL8010 rec"}},

		{`x: { x = x; }`, []string{"SL8011 x = x;"}},
		{`rec { x = x; }`, nil},
		{`let x = x; in x`, nil},
		{`y: with y; { x = x; }`, []string{"SL8011 x = x;"}},
		{`{ map = map; }`, []string{"SL8011 map = map;"}},
		{`{ x = x; }`, []string{"SL8002 x"}},
		{`x: { x.y = x; }`, nil},
		{`x: { y = x; }`, nil},
		{`{ __curPos = __curPos; }`, nil},

		{`let or = 1; f = 2; in f or`, []string{"SL8012 or"}},
		{`f: f or`, []string{"SL8002 or", "SL8012 or"}},
		{`let or = 1; in { inherit or; }`, nil},
		{`{ a = 1; }.a or 2`, nil},

		{`let in 1`, []string{"SL8013 let"}},
		{`{ inherit; }`, []string{"SL8013 inherit;"}},
		{`s: { inherit (s); }`, []string{"SL8013 inherit (s);"}},

		{`x: x == null`, nil},
		{`x: x != null`, nil},
		{`x: x == true`, nil},
		{`x: isNull x`, nil},
	} {
		if got := summaries(tt.src, findingsOf(tt.src)); !slices.Equal(got, tt.want) {
			t.Errorf("%s reports %q, want %q", tt.src, got, tt.want)
		}
	}
}

func TestChecksWaitForAFileThatParses(t *testing.T) {
	t.Parallel()

	for _, f := range findingsOf(`{ a = ; b = c; d = (1); }`) {
		if f.Code != lint.CodeNixSyntax {
			t.Errorf("a file with a syntax error also reports %s", f.Code)
		}
	}
}

func TestRepositoryNixFilesAreClean(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "..")
	var paths []string
	for _, pattern := range []string{"*.nix", filepath.Join("nix", "*.nix"), filepath.Join("tools", "*", "*.nix")} {
		found, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, found...)
	}
	for _, path := range paths {
		text, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := summaries(string(text), findingsOf(string(text))); len(got) != 0 {
			t.Errorf("%s reports %q", path, got)
		}
	}
}

func TestDirectives(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src string
		want      []string
	}{
		{"file scope", "# shellcheck disable=SL8005\nlet a = 1; in 2\n", nil},
		{"file scope after other comments", "# note\n# shellcheck disable=SL8005\nlet a = 1; in 2\n", nil},
		{"file scope reaches the whole file", "# shellcheck disable=SL8005\nlet a = let b = 1; in 2; in 3\n", nil},
		{"another code", "# shellcheck disable=SL8006\nlet a = 1; in 2\n", []string{"SL8005 a"}},
		{"a shell code", "# shellcheck disable=SC2086\nlet a = 1; in 2\n", []string{"SL8005 a"}},
		{"a list of codes", "# shellcheck disable=SL8006,SL8005\nlet a = 1; in 2\n", nil},
		{"a quoted list", "# shellcheck disable=\"SL8005\"\nlet a = 1; in 2\n", nil},
		{"a range", "# shellcheck disable=SL8000-SL8099\nlet a = 1; in 2\n", nil},
		{"all", "# shellcheck disable=all\nlet a = 1; in 2\n", nil},
		{"same line", "let\n  a = 1; # shellcheck disable=SL8005\n  b = 2;\nin 3\n", []string{"SL8005 b"}},
		{"next binding", "let\n  # shellcheck disable=SL8005\n  a = 1;\n  b = 2;\nin 3\n", []string{"SL8005 b"}},
		{"stacked above the binding", "let\n  # shellcheck disable=SL8005\n  # explains a\n  a = 1;\n  b = 2;\nin 3\n", []string{"SL8005 b"}},
		{"the whole of the next binding", "{\n  # shellcheck disable=SL8002\n  a = {\n    b = c;\n  };\n  d = e;\n}\n", []string{"SL8002 e"}},
		{"next formal", "{\n  # shellcheck disable=SL8007\n  a,\n  b,\n}: 1\n", []string{"SL8007 b"}},
		{"next list item", "x: [\n  # shellcheck disable=SL8002\n  y\n  z\n  x\n]\n", []string{"SL8002 z"}},
		{"before a keyword it reaches nothing", "let\n  a = 1;\n  # shellcheck disable=SL8005\nin 2\n", []string{"SL8005 a"}},
		{"before a closer it reaches nothing", "{\n  a = (1);\n  # shellcheck disable=SL8008\n}\n", []string{"SL8008 (1)"}},
		{"a block comment is no directive", "/* shellcheck disable=SL8005 */ let a = 1; in 2\n", []string{"SL8005 a"}},
		{"another kind of key", "# shellcheck source=SL8005\nlet a = 1; in 2\n", []string{"SL8005 a"}},
		{"a code that is no code", "# shellcheck disable=nonsense\nlet a = 1; in 2\n", []string{"SC1125 nonsense", "SL8005 a"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := summaries(tt.src, findingsOf(tt.src)); !slices.Equal(got, tt.want) {
				t.Errorf("%q reports %q, want %q", tt.src, got, tt.want)
			}
		})
	}
	if findings := findingsOf("# shellcheck disable=all\n{ a = ; }\n"); len(findings) != 1 || findings[0].Code != lint.CodeNixSyntax {
		t.Errorf("a directive changed the syntax errors: %+v", findings)
	}
}

func TestFixes(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		src, want string
		safety    lint.Safety
	}{
		{"let\n  a = 1;\n  b = 2;\nin b\n", "let\n  b = 2;\nin b\n", lint.Safe},
		{"let a = 1; b = 2; in b", "let b = 2; in b", lint.Safe},
		{"let a = 1; in 2", "let in 2", lint.Safe},
		{"let\n  a.b = 1;\n  a.c = 2;\n  d = 3;\nin d\n", "let\n  d = 3;\nin d\n", lint.Safe},
		{"x: let inherit (x) a b; in b", "x: let inherit (x) b; in b", lint.Safe},
		{"x: let inherit (x) a b; in a", "x: let inherit (x) a; in a", lint.Safe},
		{"x: let inherit (x) a; in x", "x: let in x", lint.Safe},
		{"{ a, b, ... }: a", "{ a, ... }: a", lint.Safe},
		{"{ a, b }: a", "{ a }: a", lint.Review},
		{"{ b, a }: a", "{ a }: a", lint.Review},
		{"{ a }: 1", "{ }: 1", lint.Review},
		{"{ a, b, }: a", "{ a, }: a", lint.Review},
		{"{\n  a,\n  b,\n  ...\n}: a", "{\n  a,\n  ...\n}: a", lint.Safe},
		{"{\n  a,\n  b\n}: a", "{\n  a\n}: a", lint.Review},
		{"{ a ? 1, b, ... }: b", "{ b, ... }: b", lint.Safe},
		{"{ a, a }: a", "{ a }: a", lint.Safe},
		{"{ a, a ? 1 }: a", "{ a }: a", lint.Review},
		{"a@{ b }: b", "{ b }: b", lint.Safe},
		{"a @ { b }: b", "{ b }: b", lint.Safe},
		{"{ b }@a: b", "{ b }: b", lint.Safe},
		{"{ b } @ a: b", "{ b }: b", lint.Safe},
		{"x: 1", "_x: 1", lint.Review},
		{"{ a = (1); }", "{ a = 1; }", lint.Safe},
		{"x: ( x x )", "x: x x", lint.Safe},
		{"{ a = (\n  1\n); }", "{ a = 1; }", lint.Safe},
		{"((1))", "(1)", lint.Safe},
		{"x: if x then true else false", "x: x", lint.Safe},
		{"x: if (x) then true else false", "x: x", lint.Safe},
		{"x: if x then false else true", "x: !x", lint.Safe},
		{"x: if (x) then false else true", "x: !(x)", lint.Safe},
		{"x: if x == 1 then false else true", "x: !(x == 1)", lint.Safe},
		{"rec { a = 1; }", "{ a = 1; }", lint.Safe},
		{"rec\n{ a = 1; }", "{ a = 1; }", lint.Safe},
		{"x: { x = x; }", "x: { inherit x; }", lint.Safe},
		{"let in 1", "1", lint.Safe},
		{"let\nin\n  1", "1", lint.Safe},
		{"s: { inherit (s); a = s; }", "s: { a = s; }", lint.Safe},
	} {
		findings := findingsOf(tt.src)
		i := slices.IndexFunc(findings, func(f lint.Finding) bool { return len(f.Fixes) > 0 })
		if i < 0 {
			t.Errorf("%q has no fix: %+v", tt.src, findings)
			continue
		}
		fix := findings[i].Fixes[0]
		if fix.Safety != tt.safety {
			t.Errorf("%q: the fix %q has safety %d, want %d", tt.src, fix.Title, fix.Safety, tt.safety)
		}
		got := textedit.Apply(tt.src, fix.Edits)
		if got != tt.want {
			t.Errorf("fixing %q gives %q, want %q", tt.src, got, tt.want)
		}
		if errs := syntax.Parse(got).Errors; len(errs) != 0 {
			t.Errorf("fixing %q gives %q, which does not parse: %+v", tt.src, got, errs)
		}
		if slices.Contains(summaries(got, findingsOf(got)), summaries(tt.src, findings)[i]) {
			t.Errorf("fixing %q leaves the finding in %q", tt.src, got)
		}
	}
}

func TestNoFixDropsAComment(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		"let a = /* c */ 1; in 2",
		"let # c\nin 2",
		"x: { x = /* c */ x; }",
		"x: if x /* c */ then true else false",
		"a /* c */ @ { }: 1",
		"{ a, # c\n  b }: a",
	} {
		for _, f := range findingsOf(src) {
			if len(f.Fixes) != 0 {
				t.Errorf("%q offers %+v", src, f.Fixes)
			}
		}
	}
	if findings := findingsOf("( # c\n 1)"); len(findings) != 1 || textedit.Apply("( # c\n 1)", findings[0].Fixes[0].Edits) != " # c\n 1" {
		t.Errorf("parentheses around a comment: %+v", findings)
	}
}

func TestRenamingAnArgumentCannotCapture(t *testing.T) {
	t.Parallel()

	findings := findingsOf("_x: x: _x")
	if len(findings) != 1 || findings[0].Code != lint.CodeNixUnusedArgument || len(findings[0].Fixes) != 0 {
		t.Errorf("findings = %+v", findings)
	}
}

func TestFixAllConverges(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ src, want string }{
		{"let\n  a = 1;\n  b = a;\nin 2\n", "2\n"},
		{"let\n  f = (1);\n  g = (f);\nin rec { h = (g); }\n", "let\n  f = 1;\n  g = f;\nin { h = g; }\n"},
		{"x: let inherit (x) a b c; in x", "x: x"},
		{"{ a, b, ... }: (rec { c = 1; })", "{ ... }: { c = 1; }"},
		{"x: if (if x then true else false) then false else true", "x: !(x)"},
	} {
		text := tt.src
		for range 10 {
			edits := lint.FindingsSafeEdits(findingsOf(text))
			if len(edits) == 0 {
				break
			}
			text = textedit.Apply(text, edits)
			if errs := syntax.Parse(text).Errors; len(errs) != 0 {
				t.Errorf("fixing %q gave %q, which does not parse: %+v", tt.src, text, errs)
				break
			}
		}
		if text != tt.want {
			t.Errorf("fixing %q until nothing is left gives %q, want %q", tt.src, text, tt.want)
		}
		if edits := lint.FindingsSafeEdits(findingsOf(text)); len(edits) != 0 {
			t.Errorf("fixing %q never settled: %+v", tt.src, edits)
		}
	}
}
