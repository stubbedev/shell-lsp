package highlight

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/semantic"
)

const corpusRoot = "../../../../corpus/nix"

func spell(src string, tokens []semantic.Token) []string {
	out := make([]string, len(tokens))
	for i, t := range tokens {
		out[i] = src[t.Span.Start:t.Span.End] + " " + semantic.TypeNames[t.Type]
		var mods []string
		for bit, name := range semantic.ModifierNames {
			if t.Modifiers&(1<<bit) != 0 {
				mods = append(mods, name)
			}
		}
		if len(mods) > 0 {
			out[i] += " " + strings.Join(mods, ",")
		}
	}
	return out
}

func tokensOf(src string, shell []semantic.Token, pieces ...syntax.Span) []semantic.Token {
	f := syntax.Parse(src)
	return Tokens(f, scope.Resolve(f), shell, pieces)
}

func checkShape(t *testing.T, src string, tokens []semantic.Token) {
	t.Helper()
	end := 0
	for _, tok := range tokens {
		if tok.Span.Start < end || tok.Span.End <= tok.Span.Start || tok.Span.End > len(src) {
			t.Fatalf("token %+v after offset %d is out of order or out of bounds in %d bytes", tok, end, len(src))
		}
		if strings.IndexByte(src[tok.Span.Start:tok.Span.End], '\n') >= 0 {
			t.Fatalf("token %+v spans several lines", tok)
		}
		end = tok.Span.End
	}
}

func TestClasses(t *testing.T) {
	t.Parallel()

	const (
		global   = "variable readonly,defaultLibrary"
		operator = " operator"
		number   = " number"
		str      = " string"
		location = " string external"
	)
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{"keywords and constants", "if true then 1 else 2", []string{"if keyword", "true " + global, "then keyword", "1" + number, "else keyword", "2" + number}},
		{"let binding and its use", "let a = 1; in a", []string{"let keyword", "a variable declaration", "=" + operator, "1" + number, "in keyword", "a variable"}},
		{"lambda argument and its use", "x: x", []string{"x parameter declaration", ":" + operator, "x parameter"}},
		{"formals, alias and uses", "{ a, b ? 1, ... }@args: a + b + args.c", []string{
			"a parameter declaration", "b parameter declaration", "?" + operator, "1" + number, "..." + operator, "@" + operator,
			"args parameter declaration", ":" + operator, "a parameter", "+" + operator, "b parameter", "+" + operator, "args parameter", "." + operator, "c property",
		}},
		{"global functions", "builtins.map toString [ null ]", []string{"builtins namespace defaultLibrary", "." + operator, "map function defaultLibrary", "toString function defaultLibrary", "null " + global}},
		{"builtins constant member", "builtins.currentSystem", []string{"builtins namespace defaultLibrary", "." + operator, "currentSystem " + global}},
		{"unknown builtins member", "builtins.nope", []string{"builtins namespace defaultLibrary", "." + operator, "nope property"}},
		{"hidden alias is no member", "builtins.__add", []string{"builtins namespace defaultLibrary", "." + operator, "__add property"}},
		{"parenthesised builtins", "(builtins).map", []string{"builtins namespace defaultLibrary", "." + operator, "map function defaultLibrary"}},
		{"has attribute on builtins", "builtins ? map", []string{"builtins namespace defaultLibrary", "?" + operator, "map function defaultLibrary"}},
		{"shadowed builtins", "builtins: builtins.map", []string{"builtins parameter declaration", ":" + operator, "builtins parameter", "." + operator, "map property"}},
		{"name through with", "with lib; foo", []string{"with keyword", "foo variable external"}},
		{"lexical binding beats with", "x: with lib; x", []string{"x parameter declaration", ":" + operator, "with keyword", "x parameter"}},
		{"global beats with", "with lib; map", []string{"with keyword", "map function defaultLibrary"}},
		{"member through with builtins", "with builtins; attrNames", []string{"with keyword", "builtins namespace defaultLibrary", "attrNames function defaultLibrary"}},
		{"unknown through with builtins", "with builtins; nope", []string{"with keyword", "builtins namespace defaultLibrary", "nope variable external"}},
		{"undefined name", "nope", []string{}},
		{"selection on an undefined name", "a.b", []string{"." + operator, "b property"}},
		{"select with a default", "a: a.b or 1", []string{"a parameter declaration", ":" + operator, "a parameter", "." + operator, "b property", "or keyword", "1" + number}},
		{"attribute names", `{ a.b = 1; "c d" = 2; ${e} = 3; inherit f; inherit (g) h; or = 4; }`, []string{
			"a property declaration", "." + operator, "b property declaration", "=" + operator, "1" + number,
			`"` + str, "c d property declaration", `"` + str, "=" + operator, "2" + number,
			"${" + operator, "}" + operator, "=" + operator, "3" + number,
			"inherit keyword", "f property declaration", "inherit keyword", "h property declaration",
			"or property declaration", "=" + operator, "4" + number,
		}},
		{"recursive set", "rec { a = 1; b = a; }", []string{"rec keyword", "a property declaration", "=" + operator, "1" + number, "b property declaration", "=" + operator, "a property"}},
		{"inherit into let", "let inherit (x) y; in y", []string{"let keyword", "inherit keyword", "y variable declaration", "in keyword", "y variable"}},
		{"nested path in let", "let a.b = 1; in a", []string{"let keyword", "a variable declaration", "." + operator, "b property declaration", "=" + operator, "1" + number, "in keyword", "a variable"}},
		{"quoted string", `"a\n${1}b"`, []string{`"` + str, "a" + str, `\n regexp`, "${" + operator, "1" + number, "}" + operator, "b" + str, `"` + str}},
		{"multi-line indented string", "''\n  x\n  ''${1}\n''", []string{"''" + str, "x" + str, "''$ regexp", "{1}" + str, "''" + str}},
		{"paths and URIs", "[ ./a/b <nixpkgs> ~/x /abs https://x.y/z ]", []string{"./a/b" + location, "<nixpkgs>" + location, "~/x" + location, "/abs" + location, "https://x.y/z" + location}},
		{"interpolated path", "./a/${b}/c", []string{"./a/" + location, "${" + operator, "}" + operator, "/c" + location}},
		{"numbers", "[ 1 2.5 ]", []string{"1" + number, "2.5" + number}},
		{"operators", "!true || false -> 1 + 2 * 3 == 4", []string{
			"!" + operator, "true " + global, "||" + operator, "false " + global, "->" + operator,
			"1" + number, "+" + operator, "2" + number, "*" + operator, "3" + number, "==" + operator, "4" + number,
		}},
		{"update", "{} // {}", []string{"//" + operator}},
		{"concat", "[] ++ []", []string{"++" + operator}},
		{"pipe", "a |> b", []string{"|>" + operator}},
		{"division and comparison", "1 / 2 < 3", []string{"1" + number, "/" + operator, "2" + number, "<" + operator, "3" + number}},
		{"comments", "# a\n/* b\n   c */\n1", []string{"# a comment", "/* b comment", "c */ comment", "1" + number}},
		{"missing value", "let a = ; in a", []string{"let keyword", "a variable declaration", "=" + operator, "in keyword", "a variable"}},
		{"unterminated string", `{ a = "x`, []string{"a property declaration", "=" + operator, `"` + str, "x" + str}},
		{"unterminated antiquotation", `"a${`, []string{`"` + str, "a" + str, "${" + operator}},
		{"unterminated comment", "/* open", []string{"/* open comment"}},
		{"invalid byte before a stray expression", "% 1", []string{"1" + number}},
		{"lone keyword", "if", []string{"if keyword"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tokens := tokensOf(tc.src, nil)
			checkShape(t, tc.src, tokens)
			if got := spell(tc.src, tokens); !slices.Equal(got, tc.want) {
				t.Errorf("tokens of %q\n got %q\nwant %q", tc.src, got, tc.want)
			}
		})
	}
}

func TestShellComposition(t *testing.T) {
	t.Parallel()

	type shellToken struct {
		text string
		kind semantic.Type
		mods semantic.Modifier
	}
	const (
		function = semantic.TypeFunction
		keyword  = semantic.TypeKeyword
		variable = semantic.TypeVariable
	)
	cases := []struct {
		name  string
		src   string
		shell []shellToken
		want  []string
	}{
		{
			"shell wins and antiquotations stay Nix",
			`x: "echo ${x} $y"`,
			[]shellToken{{"echo", function, semantic.ModDefaultLibrary}, {"$y", variable, 0}},
			[]string{"x parameter declaration", ": operator", `" string`, "echo function defaultLibrary", "${ operator", "x parameter", "} operator", "$y variable", `" string`},
		},
		{
			"text no shell token claims stays a Nix string",
			`"echo hi"`,
			[]shellToken{{"echo", function, 0}},
			[]string{`" string`, "echo function", "hi string", `" string`},
		},
		{
			"a shell word is cut around an antiquotation",
			`x: "a${x}b"`,
			[]shellToken{{"a${x}b", function, 0}},
			[]string{"x parameter declaration", ": operator", `" string`, "a function", "${ operator", "x parameter", "} operator", "b function", `" string`},
		},
		{
			"a shell token on the placeholder itself disappears",
			`x: "${x}"`,
			[]shellToken{{"${x}", variable, 0}},
			[]string{"x parameter declaration", ": operator", `" string`, "${ operator", "x parameter", "} operator", `" string`},
		},
		{
			"a shell string nested in an antiquotation keeps its own tokens",
			`x: "a${ "b${x}" }c"`,
			[]shellToken{{`a${ "b${x}" }c`, function, 0}, {"b", keyword, 0}},
			[]string{"x parameter declaration", ": operator", `" string`, "a function", "${ operator", `" string`, "b keyword", "${ operator", "x parameter", "} operator", `" string`, "} operator", "c function", `" string`},
		},
		{
			"a shell word across a spliced string colours the text it holds",
			`c: "foo${f c "bar"} x"`,
			[]shellToken{{`foo${f c "bar"}`, function, 0}},
			[]string{"c parameter declaration", ": operator", `" string`, "foo function", "${ operator", "c parameter", `" string`, "bar function", `" string`, "} operator", " x string", `" string`},
		},
		{
			"a string in a splice that is no shell stays a Nix string",
			`c: "foo${f "no" "bar"} x"`,
			[]shellToken{{`foo${f "no" "bar"}`, function, 0}},
			[]string{"c parameter declaration", ": operator", `" string`, "foo function", "${ operator", `" string`, "no string", `" string`, `" string`, "bar function", `" string`, "} operator", " x string", `" string`},
		},
		{
			"a shell token never reaches past its string",
			`"ab" + "cd"`,
			[]shellToken{{`ab" + "`, function, 0}},
			[]string{`" string`, "ab function", `" string`, "+ operator", `" string`, "cd string", `" string`},
		},
		{
			"a shell token keeps the Nix escapes inside it in one piece",
			`"a\"b"`,
			[]shellToken{{`a\"b`, function, 0}},
			[]string{`" string`, `a\"b function`, `" string`},
		},
		{
			"a shell token over several source lines is split",
			"''\n  a\n  b\n''",
			[]shellToken{{"a\n  b", function, 0}},
			[]string{"'' string", "a function", "b function", "'' string"},
		},
		{
			"a shell token outside every string is ignored",
			"1",
			[]shellToken{{"1", keyword, 0}},
			[]string{"1 number"},
		},
	}
	spliced := map[string]string{
		"a shell string nested in an antiquotation keeps its own tokens": "b",
		"a shell word across a spliced string colours the text it holds": "bar",
		"a string in a splice that is no shell stays a Nix string":       "bar",
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var pieces []syntax.Span
			if text, nested := spliced[tc.name]; nested {
				at := strings.Index(tc.src, text)
				pieces = []syntax.Span{{Start: at, End: at + len(text)}}
			}
			shell := make([]semantic.Token, len(tc.shell))
			for i, s := range tc.shell {
				at := strings.Index(tc.src, s.text)
				if at < 0 {
					t.Fatalf("%q is not in %q", s.text, tc.src)
				}
				shell[i] = semantic.Token{Span: syntax.Span{Start: at, End: at + len(s.text)}, Type: s.kind, Modifiers: s.mods}
			}
			tokens := tokensOf(tc.src, shell, pieces...)
			checkShape(t, tc.src, tokens)
			if got := spell(tc.src, tokens); !slices.Equal(got, tc.want) {
				t.Errorf("tokens of %q\n got %q\nwant %q", tc.src, got, tc.want)
			}
		})
	}
}

func TestDeepNestingStaysWellFormed(t *testing.T) {
	t.Parallel()

	src := strings.Repeat("[ ", 100_000) + "1"
	checkShape(t, src, tokensOf(src, nil))
}

func TestCorpus(t *testing.T) {
	t.Parallel()

	err := filepath.WalkDir(corpusRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".nix" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(raw)
		tokens := tokensOf(src, nil)
		checkShape(t, src, tokens)
		if len(tokens) == 0 {
			t.Errorf("%s has no tokens", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(corpusRoot, "flake-parts", "pkgs", "mirrorctl.nix"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	got := spell(src, tokensOf(src, nil))
	for _, want := range []string{
		"lib parameter declaration", "syncTools parameter declaration", "stdenvNoCC parameter", "mkDerivation property",
		"pname property declaration", "inherit keyword", "version property declaration", "buildPhase property declaration",
		"lib parameter", "makeBinPath property", "syncTools parameter", "${ operator", "} operator",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("no %q among %q", want, got)
		}
	}
}
