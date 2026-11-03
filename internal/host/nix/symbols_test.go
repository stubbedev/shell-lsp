package nix

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

func outline(ix *model.Index) []string {
	var out []string
	var walk func(parent int, depth string)
	walk = func(parent int, depth string) {
		for _, i := range ix.Children(parent) {
			s := ix.Symbols[i]
			out = append(out, depth+s.Name+":"+s.Kind.String())
			walk(int(i), depth+"  ")
		}
	}
	walk(model.NoParent, "")
	return out
}

func TestOutline(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src string
		want      []string
	}{
		{name: "an empty file", src: ""},
		{
			name: "attribute sets nest",
			src:  "{ top.nested = 1; set = { inner = 2; }; \"quoted\" = 3; ${dynamic} = 4; }",
			want: []string{"top:attribute", "  nested:attribute", "set:attribute", "  inner:attribute", "quoted:attribute"},
		},
		{
			name: "a rec set is a set",
			src:  "rec { one = 1; two = one; }",
			want: []string{"one:attribute", "two:attribute"},
		},
		{
			name: "a let block holds its bindings and the body follows",
			src:  "let one = 1; fun = arg: arg; in { two = one; }",
			want: []string{"let:module", "  one:variable", "  fun:function", "    arg:parameter", "two:attribute"},
		},
		{
			name: "a function holds its parameters and body",
			src:  "{ fun = { first, second ? { inner = 1; }, ... } @ args: { value = first; }; }",
			want: []string{"fun:function", "  first:parameter", "  second:parameter", "  args:parameter", "  inner:attribute", "  value:attribute"},
		},
		{
			name: "the parameters of the file are at the top",
			src:  "{ pkgs }: self: { one = 1; }",
			want: []string{"pkgs:parameter", "self:parameter", "one:attribute"},
		},
		{
			name: "a call in a value belongs to the attribute",
			src:  "{ packages = forAll (system: let pkgs = 1; in { default = pkgs; }); }",
			want: []string{"packages:attribute", "  system:parameter", "  let:module", "    pkgs:variable", "  default:attribute"},
		},
		{
			name: "inherit binds",
			src:  "let one = 1; in { inherit one; inherit (x) two three; }",
			want: []string{"let:module", "  one:variable", "one:attribute", "two:attribute", "three:attribute"},
		},
		{
			name: "a legacy let",
			src:  "let { one = 1; body = one; }",
			want: []string{"let:module", "  one:variable", "  body:variable"},
		},
		{
			name: "values in every position",
			src:  "[ (if c then { a = 1; } else { b = 2; }) { c = 3; }.c \"${let d = 1; in d}\" ]",
			want: []string{"a:attribute", "b:attribute", "c:attribute", "let:module", "  d:variable"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := outline(Analyze(tt.src).Index); !slices.Equal(got, tt.want) {
				t.Errorf("outline =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func selections(ix *model.Index, name string) []model.Span {
	var out []model.Span
	for _, s := range ix.Symbols {
		if s.Name == name {
			out = append(out, s.Selection)
		}
	}
	return out
}

func TestAnExportedBindingIsOneSymbol(t *testing.T) {
	t.Parallel()

	src := "stdenv.mkDerivation rec {\n  pname = \"x\";\n  name = pname;\n  env.CFLAGS = pname;\n  meta.description = \"\";\n  buildPhase = ''\n    echo $pname $CFLAGS\n  '';\n}\n"
	a := Analyze(src)
	if got := selections(a.Index, "pname"); len(got) != 1 {
		t.Fatalf("pname is %d symbols: %+v", len(got), got)
	}
	var kinds []string
	for _, s := range a.Index.Symbols {
		if s.Exported {
			kinds = append(kinds, s.Name)
			if s.Kind != model.KindVariable || !slices.Contains(a.Index.Extends[a.Index.Shells[0].Scope], s.Scope) {
				t.Errorf("exported %+v is not a variable in a scope the shell reads", s)
			}
		}
	}
	if want := []string{"pname", "name", "CFLAGS", "buildPhase"}; !slices.Equal(kinds, want) {
		t.Errorf("exported %q, want %q", kinds, want)
	}
	if got := selections(a.Index, "description"); len(got) != 1 {
		t.Errorf("description is %d symbols", len(got))
	}
}

func TestASymbolUnderAnInheritIsNotWhereNavigationStops(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src string
		passive   bool
	}{
		{name: "a binding read by the inherit", src: "let one = 1; in { inherit one; }", passive: true},
		{name: "nothing read by the inherit", src: "{ inherit one; }"},
		{name: "a builtin read by the inherit", src: "{ inherit map; }"},
		{name: "an unknown set", src: "{ inherit (x) one; }"},
		{name: "a known set", src: "let x = { one = 1; }; in { inherit (x) one; }", passive: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ix := Analyze(tt.src).Index
			i := len(ix.Symbols) - 1
			if ix.Symbols[i].Passive != tt.passive {
				t.Errorf("symbol %+v: passive = %v, want %v", ix.Symbols[i], ix.Symbols[i].Passive, tt.passive)
			}
		})
	}
}

func TestFolds(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src string
		want      []string
	}{
		{name: "one line folds nothing", src: "{ a = [ 1 ]; b = ''x''; c = let d = 1; in d; }"},
		{name: "an attribute set", src: "{\n  a = 1;\n}\n", want: []string{"{\n  a = 1;\n}"}},
		{name: "a list", src: "[\n  1\n]\n", want: []string{"[\n  1\n]"}},
		{name: "a string", src: "''\n  x\n''\n", want: []string{"''\n  x\n''"}},
		{name: "a let stops at its last binding", src: "let\n  a = 1;\nin\n  a\n", want: []string{"let\n  a = 1;"}},
		{name: "a lambda", src: "x:\n  x\n", want: []string{"x:\n  x"}},
		{name: "a pattern", src: "{\n  a,\n}: a\n", want: []string{"{\n  a,\n}: a", "{\n  a,\n}"}},
		{name: "a run of comments", src: "# a\n# b\n\n# c\n1\n", want: []string{"# a\n# b", "# c"}},
		{name: "a block comment", src: "/* a\n b */\n1\n", want: []string{"/* a\n b */"}},
		{name: "a block comment is not in a run", src: "# a\n/* b */\n1\n", want: []string{"# a", "/* b */"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, f := range Analyze(tt.src).Index.Folds {
				text := tt.src[f.Span.Start:f.Span.End]
				if strings.Contains(text, "\n") || f.Comment {
					got = append(got, text)
				}
				if strings.HasPrefix(text, "#") != f.Comment && !strings.HasPrefix(text, "/*") {
					t.Errorf("fold %q: comment = %v", text, f.Comment)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("folds = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEnclosing(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src, at string
		want          []string
	}{
		{
			name: "an identifier in a binding in a set",
			src:  "{\n  a = [ one two ];\n}\n",
			at:   "two",
			want: []string{"two", "[ one two ]", "a = [ one two ];", "{\n  a = [ one two ];\n}", "{\n  a = [ one two ];\n}\n"},
		},
		{
			name: "an attribute path",
			src:  "{ a.bcd = 1; }",
			at:   "bcd",
			want: []string{"bcd", "a.bcd", "a.bcd = 1;", "{ a.bcd = 1; }"},
		},
		{
			name: "text in a string",
			src:  "{ a = \"xyz ${b}\"; }",
			at:   "xyz",
			want: []string{"xyz ", "xyz ${b}", "\"xyz ${b}\"", "a = \"xyz ${b}\";", "{ a = \"xyz ${b}\"; }"},
		},
		{
			name: "in an antiquotation",
			src:  "\"${f arg}\"",
			at:   "arg",
			want: []string{"arg", "f arg", "${f arg}", "\"${f arg}\""},
		},
		{
			name: "a let and a lambda",
			src:  "x: let y = x; in yy",
			at:   "yy",
			want: []string{"yy", "let y = x; in yy", "x: let y = x; in yy"},
		},
		{
			name: "a comment",
			src:  "# note\n1\n",
			at:   "note",
			want: []string{"# note", "# note\n1\n"},
		},
		{
			name: "a formal",
			src:  "{ abc ? 1 }: abc",
			at:   "abc",
			want: []string{"abc", "abc ? 1", "{ abc ? 1 }", "{ abc ? 1 }: abc"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, span := range Analyze(tt.src).Enclosing(strings.Index(tt.src, tt.at) + 1) {
				got = append(got, tt.src[span.Start:span.End])
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("enclosing =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestValidName(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]bool{
		"a": true, "a-b": true, "a'": true, "_a": true, "a1": true, "true": true,
		"": false, "1a": false, "a b": false, "a.b": false, "let": false, "in": false, "or": false, "rec": false,
		"__curPos": false, "__overrides": false, "__findFile": false, "./a": false, "a:b": false, "\"a\"": false, "#a": false,
	} {
		if got := ValidName(model.KindVariable, name); got != want {
			t.Errorf("ValidName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestCorpusSymbolsAreSound(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "..", "corpus", "nix")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".nix" {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(raw)
		a := Analyze(src)
		seen := map[model.Span]bool{}
		for _, s := range a.Index.Symbols {
			if s.Passive && s.Name == letBlockName {
				continue
			}
			if seen[s.Selection] {
				t.Errorf("%s: two symbols are named at %q", path, src[s.Selection.Start:s.Selection.End])
			}
			seen[s.Selection] = true
			if src[s.Selection.Start:s.Selection.End] != s.Name || s.Selection.Start < s.Span.Start || s.Selection.End > s.Span.End {
				t.Errorf("%s: symbol %+v is not where it is named", path, s)
			}
		}
		refs := map[model.Span]bool{}
		for _, r := range a.Index.Refs {
			refs[r.Span] = true
		}
		for _, u := range a.Scope.Uses() {
			if u.Ref.Kind != scope.Definition {
				continue
			}
			span := u.Node.Span()
			if attr, ok := u.Node.(syntax.Attr); ok {
				span = nameSpan(attr)
			}
			if !refs[span] {
				t.Errorf("%s: the read of %s at %d has no reference", path, u.Name, span.Start)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
