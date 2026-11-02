package hostnav

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

const nixFile = "default.nix"

func nixTree(t *testing.T, src string) *tree {
	t.Helper()
	return newTree(t, map[string]string{nixFile: src})
}

func TestNixReferencesCoverEveryKindOfBinding(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src, sub string
		nth            int
		want           []int
	}{
		{name: "let", src: "let\n  one = 1;\nin\n  one + one\n", sub: "one", nth: 1, want: []int{2, 4, 4}},
		{name: "legacy let", src: "let {\n  one = 1;\n  body = one;\n}\n", sub: "one", nth: 1, want: []int{2, 3}},
		{name: "rec attribute", src: "rec {\n  one = 1;\n  two = one;\n}\n", sub: "one", nth: 1, want: []int{2, 3}},
		{name: "lambda argument", src: "arg:\n  arg\n", sub: "arg", nth: 1, want: []int{1, 2}},
		{name: "formal", src: "{\n  first,\n  second ? first,\n}:\n  first + second\n", sub: "first", nth: 2, want: []int{2, 3, 5}},
		{name: "alias before the formals", src: "args @ {\n  first,\n  ...\n}:\n  args\n", sub: "args", nth: 1, want: []int{1, 5}},
		{name: "alias after the formals", src: "{\n  first,\n  ...\n} @ args:\n  args\n", sub: "args", nth: 0, want: []int{4, 5}},
		{name: "a formal read through its alias", src: "{\n  first,\n  ...\n} @ args:\n  args.first\n", sub: "first", nth: 1, want: []int{2, 5}},
		{name: "use of a let inherit", src: "let\n  one = 1;\nin let\n  inherit one;\nin\n  one\n", sub: "one", nth: 2, want: []int{4, 6}},
		{name: "a let inherit reads what it names", src: "let\n  one = 1;\nin let\n  inherit one;\nin\n  one\n", sub: "one", nth: 1, want: []int{2, 4}},
		{name: "an attribute inherit reads what it names", src: "let\n  one = 1;\nin {\n  inherit one;\n}\n", sub: "one", nth: 1, want: []int{2, 4}},
		{name: "shadowing keeps the outer binding", src: "let\n  one = 1;\nin [\n  one\n  (one: one)\n]\n", sub: "one", nth: 1, want: []int{2, 4}},
		{name: "shadowing binds the inner one", src: "let\n  one = 1;\nin [\n  one\n  (one: one)\n]\n", sub: "one", nth: 2, want: []int{5, 5}},
		{name: "with does not beat a binding", src: "let\n  one = 1;\nin with pkgs;\n  one\n", sub: "one", nth: 1, want: []int{2, 4}},
		{name: "an attribute read through its let", src: "let\n  set = {\n    one = 1;\n  };\nin\n  set.one\n", sub: "one", nth: 1, want: []int{3, 6}},
		{name: "an attribute from its binding", src: "let\n  set = {\n    one = 1;\n  };\nin\n  set.one\n", sub: "one", nth: 0, want: []int{3, 6}},
		{name: "a nested attribute path", src: "let\n  set.sub.one = 1;\nin\n  set.sub.one\n", sub: "one", nth: 1, want: []int{2, 4}},
		{name: "a merged attribute set", src: "let\n  set = { one = 1; };\n  set.two = 2;\nin\n  set.one + set.two\n", sub: "two", nth: 1, want: []int{3, 5}},
		{name: "a has-attr test", src: "let\n  set = {\n    one = 1;\n  };\nin\n  set ? one\n", sub: "one", nth: 1, want: []int{3, 6}},
		{name: "a set literal selected in place", src: "{\n  one = 1;\n}.one\n", sub: "one", nth: 1, want: []int{2, 3}},
		{name: "inherit from a known set", src: "let\n  set = {\n    one = 1;\n  };\nin {\n  inherit (set) one;\n}\n", sub: "one", nth: 1, want: []int{3, 6}},
		{name: "with over a known set", src: "let\n  set = {\n    one = 1;\n  };\nin\n  with set; one\n", sub: "one", nth: 1, want: []int{3, 6}},
		{name: "a call passes a formal", src: "let\n  fun = { one }: one;\nin\n  fun {\n    one = 1;\n  }\n", sub: "one", nth: 2, want: []int{2, 2, 5}},
		{name: "through an antiquotation", src: "let\n  one = 1;\nin\n  \"${toString one}\"\n", sub: "one", nth: 1, want: []int{2, 4}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tr := nixTree(t, tt.src)
			g, sym := tr.at(nixFile, tt.sub, tt.nth)
			var got []int
			for _, o := range g.Occurrences(sym) {
				got = append(got, 1+strings.Count(tt.src[:o.Span.Start], "\n"))
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("occurrences on lines %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNixDefinitionGoesToTheBinding(t *testing.T) {
	t.Parallel()

	src := "let\n  one = 1;\n  fun = { one }: one;\nin {\n  inherit one;\n  two = fun { one = one; };\n}\n"
	tr := nixTree(t, src)
	for _, tt := range []struct {
		nth  int
		line string
	}{
		{0, "default.nix:2:one"},
		{1, "default.nix:3:one"},
		{2, "default.nix:3:one"},
		{3, "default.nix:2:one"},
		{4, "default.nix:3:one"},
		{5, "default.nix:2:one"},
	} {
		if got := tr.locations(tr.group(nixFile).Definition(tr.offset(nixFile, "one", tt.nth))); !slices.Equal(got, []string{tt.line}) {
			t.Errorf("definition of `one` #%d = %v, want %s", tt.nth, got, tt.line)
		}
	}
}

func TestNixNamesThatCannotBeProvenHaveNoTarget(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src, sub string
		nth            int
	}{
		{name: "self is an argument", src: "{ self }: {\n  one = 1;\n  two = self.one;\n}\n", sub: "one", nth: 1},
		{name: "a with over an unknown set", src: "with pkgs; hello\n", sub: "hello"},
		{name: "a builtin", src: "map\n", sub: "map"},
		{name: "a set behind an update", src: "let\n  set = { one = 1; } // other;\nin\n  set.one\n", sub: "one", nth: 1},
		{name: "a dynamic attribute before it", src: "let\n  set = { sub.one = 1; };\nin\n  set.${key}.one\n", sub: "one", nth: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tr := nixTree(t, tt.src)
			if sym, ok := tr.group(nixFile).At(tr.offset(nixFile, tt.sub, tt.nth)); ok {
				t.Errorf("found %+v", sym)
			}
		})
	}
}

func renameNix(t *testing.T, src, sub string, nth int, newName string) (string, error) {
	t.Helper()
	tr := nixTree(t, src)
	g, sym := tr.at(nixFile, sub, nth)
	occs, err := g.Rename(sym, newName)
	if err != nil {
		return "", err
	}
	var edits []textedit.Edit
	for _, o := range occs {
		edits = append(edits, o.Edits(newName)...)
	}
	return textedit.Apply(src, edits), nil
}

func TestNixRename(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src, sub string
		nth            int
		want           string
	}{
		{name: "let", src: "let one = 1; in one + one", sub: "one", want: "let alpha = 1; in alpha + alpha"},
		{name: "from a read", src: "let one = 1; in one + one", sub: "one", nth: 2, want: "let alpha = 1; in alpha + alpha"},
		{name: "rec attribute", src: "rec { one = 1; two = one; }", sub: "one", want: "rec { alpha = 1; two = alpha; }"},
		{name: "lambda argument", src: "one: one", sub: "one", want: "alpha: alpha"},
		{name: "formal", src: "{ one, two ? one }: one", sub: "one", want: "{ alpha, two ? alpha }: alpha"},
		{name: "alias", src: "one @ { two, ... }: one", sub: "one", want: "alpha @ { two, ... }: alpha"},
		{name: "a formal read through its alias", src: "{ one, ... } @ args: args.one", sub: "one", want: "{ alpha, ... } @ args: args.alpha"},
		{name: "only the shadowed binding", src: "let one = 1; in [ one (one: one) ]", sub: "one", nth: 2, want: "let one = 1; in [ one (alpha: alpha) ]"},
		{name: "through an antiquotation", src: "let one = 1; in \"${toString one}\"", sub: "one", want: "let alpha = 1; in \"${toString alpha}\""},
		{name: "a lone inherit keeps its attribute", src: "let one = 1; in { inherit one; }", sub: "one", want: "let alpha = 1; in { one = alpha; }"},
		{name: "an inherit keeps its attribute from the inherit itself", src: "let one = 1; in { inherit one; }", sub: "one", nth: 1, want: "let alpha = 1; in { one = alpha; }"},
		{name: "the first of several inherited", src: "let one = 1; in { inherit one two; }", sub: "one", want: "let alpha = 1; in { inherit two; one = alpha; }"},
		{name: "the last of several inherited", src: "let one = 1; in { inherit two one; }", sub: "one", want: "let alpha = 1; in { inherit two; one = alpha; }"},
		{name: "a binding read by a let inherit", src: "let one = 1; in let inherit one; in one", sub: "one", want: "let alpha = 1; in let one = alpha; in one"},
		{name: "the binding of a let inherit", src: "let one = 1; in let inherit one; in one", sub: "one", nth: 2, want: "let one = 1; in let alpha = one; in alpha"},
		{name: "the binding of an inherit from", src: "let inherit (set) one; in one", sub: "one", nth: 1, want: "let alpha = (set).one; in alpha"},
		{name: "one binding of an inherit from", src: "let inherit (set) one two; in one", sub: "one", nth: 1, want: "let inherit (set) two; alpha = (set).one; in alpha"},
		{name: "an attribute only its let reads", src: "let set = { one = 1; }; in set.one", sub: "one", want: "let set = { alpha = 1; }; in set.alpha"},
		{name: "a nested attribute only its let reads", src: "let set.sub.one = 1; in set.sub.one", sub: "one", want: "let set.sub.alpha = 1; in set.sub.alpha"},
		{name: "a quoted attribute", src: "let set = { \"one\" = 1; }; in set.one", sub: "one", want: "let set = { \"alpha\" = 1; }; in set.alpha"},
		{name: "an attribute an inherit from reads", src: "let set = { one = 1; }; in let inherit (set) one; in one", sub: "one", want: "let set = { alpha = 1; }; in let one = (set).alpha; in one"},
		{name: "a rec attribute read from outside", src: "let set = rec { one = 1; two = one; }; in set.one", sub: "one", want: "let set = rec { alpha = 1; two = alpha; }; in set.alpha"},
		{name: "a formal and the calls in the file", src: "let fun = { one }: one; in fun { one = 1; }", sub: "one", want: "let fun = { alpha }: alpha; in fun { alpha = 1; }"},
		{name: "a formal from its call", src: "let fun = { one }: one; in fun { one = 1; }", sub: "one", nth: 2, want: "let fun = { alpha }: alpha; in fun { alpha = 1; }"},
		{name: "a formal passed by inherit", src: "let one = 1; fun = { one }: one; in fun { inherit one; }", sub: "one", nth: 1, want: "let one = 1; fun = { alpha }: alpha; in fun { alpha = one; }"},
		{name: "a formal of a curried call", src: "let fun = x: { one }: one; in fun 1 { one = 1; }", sub: "one", want: "let fun = x: { alpha }: alpha; in fun 1 { alpha = 1; }"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := renameNix(t, tt.src, tt.sub, tt.nth, "alpha")
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("renamed to\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestNixRenameKeepsANameThatIsAlreadyRight(t *testing.T) {
	t.Parallel()

	if got, err := renameNix(t, "let one = 1; in one", "one", 0, "one"); err != nil || got != "let one = 1; in one" {
		t.Errorf("renaming to the same name = %q, %v", got, err)
	}
}

func TestNixRenameRefusals(t *testing.T) {
	t.Parallel()

	derivation := "stdenv.mkDerivation rec {\n  pname = \"x\";\n  name = pname;\n  buildPhase = ''\n    echo $pname\n  '';\n}\n"
	for _, tt := range []struct {
		name, src, sub string
		nth            int
	}{
		{name: "__overrides is read by the evaluator", src: "rec { __overrides = { }; one = 1; }", sub: "__overrides"},
		{name: "body is read by the evaluator", src: "let { body = 1; }", sub: "body"},
		{name: "an attribute of the value of the file", src: "{ one = 1; }", sub: "one"},
		{name: "an attribute of a set that is returned", src: "let set = { one = 1; }; in set", sub: "one"},
		{name: "an attribute of a set passed on", src: "let set = { one = 1; }; in fun set", sub: "one"},
		{name: "an attribute of a set opened by with", src: "let set = { one = 1; }; in with set; one", sub: "one"},
		{name: "an attribute of a set read by a dynamic name", src: "let set = { one = 1; }; in set.${key}", sub: "one"},
		{name: "an attribute below a value that leaves", src: "let set = { sub = { one = 1; }; }; in set.sub", sub: "one"},
		{name: "a function result", src: "{ self }: { one = 1; two = self.one; }", sub: "one"},
		{name: "an inherit with nothing behind it", src: "{ inherit one; }", sub: "one"},
		{name: "a derivation attribute the shell reads", src: derivation, sub: "pname"},
		{name: "a derivation attribute from its read", src: derivation, sub: "pname", nth: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := renameNix(t, tt.src, tt.sub, tt.nth, "alpha"); !errors.Is(err, navigation.ErrNotRenamable) {
				t.Errorf("error = %v, want %v", err, navigation.ErrNotRenamable)
			}
		})
	}
	tr := nixTree(t, derivation)
	g, sym := tr.shellAt(nixFile, "$pname", 0)
	if err := g.PrepareRename(sym); !errors.Is(err, navigation.ErrNotRenamable) {
		t.Errorf("renaming from the shell = %v, want %v", err, navigation.ErrNotRenamable)
	}
}

func TestNixRenameConflicts(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, src, newName string }{
		{name: "the same scope", src: "let one = 1; two = 2; in one + two", newName: "two"},
		{name: "an inner binding would capture a read", src: "let one = 1; in let two = 2; in one + two", newName: "two"},
		{name: "a lambda would capture a read", src: "let one = 1; in two: one + two", newName: "two"},
		{name: "a read of an outer binding would be shadowed", src: "let two = 1; in let one = 2; in one + two", newName: "two"},
		{name: "a builtin would be shadowed", src: "let one = 1; in map one", newName: "map"},
		{name: "a name from with would be shadowed", src: "let one = 1; in with pkgs; one + two", newName: "two"},
		{name: "an undefined name would be shadowed", src: "let one = 1; in one + two", newName: "two"},
		{name: "the value of a moved inherit would be captured", src: "let one = 1; in let two = 2; inherit one; in two", newName: "two"},
		{name: "an attribute next to it", src: "let set = { one = 1; two = 2; }; in set.one + set.two", newName: "two"},
		{name: "a read that finds nothing now", src: "let set = { one = 1; }; in set.one + set.two", newName: "two"},
		{name: "a read through the alias that finds nothing now", src: "{ one, ... } @ args: one + args.two", newName: "two"},
		{name: "the alias", src: "{ one, ... } @ two: one", newName: "two"},
		{name: "a key a call passes next to it", src: "let fun = { one, ... }: one; in fun { one = 1; two = 2; }", newName: "two"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got, err := renameNix(t, tt.src, "one", 0, tt.newName); !errors.Is(err, navigation.ErrNameConflict) {
				t.Errorf("renamed to %q, error %v, want %v", got, err, navigation.ErrNameConflict)
			}
		})
	}
	if got, err := renameNix(t, "let two = 1; in [ two (let one = 2; in one) ]", "one", 0, "two"); err != nil || got != "let two = 1; in [ two (let two = 2; in two) ]" {
		t.Errorf("a binding that shadows nothing it can see = %q, %v", got, err)
	}
}

func TestNixRenameValidatesTheName(t *testing.T) {
	t.Parallel()

	for _, newName := range []string{"let", "if", "or", "inherit", "1bad", "a b", "a.b", "", "__curPos", "__overrides", "__sub", "\"x\"", "./x", "a:b"} {
		if _, err := renameNix(t, "let one = 1; in one", "one", 0, newName); !errors.Is(err, navigation.ErrInvalidName) {
			t.Errorf("renaming to %q: error %v, want %v", newName, err, navigation.ErrInvalidName)
		}
	}
	for _, newName := range []string{"a-b", "a'", "_x", "true", "A1"} {
		if _, err := renameNix(t, "let one = 1; in one", "one", 0, newName); err != nil {
			t.Errorf("renaming to %q: %v", newName, err)
		}
	}
}

func corpusTree(t *testing.T, name string) *tree {
	t.Helper()
	root := filepath.Join("..", "..", "corpus", "nix", name)
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".nix" {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return newTree(t, files)
}

func (tr *tree) after(name, before, sub string) int {
	tr.t.Helper()
	return tr.offset(name, before+sub, 0) + len(before)
}

func (tr *tree) site(file, before, name string) string {
	tr.t.Helper()
	text := tr.files[file]
	if n := strings.Count(text, before+name); n != 1 {
		tr.t.Fatalf("%q occurs %d times in %s, want once", before+name, n, file)
	}
	return fmt.Sprintf("%s:%d:%s", file, strings.Count(text[:tr.after(file, before, name)], "\n")+1, name)
}

func (tr *tree) definitionAfter(name, before, sub string) []string {
	tr.t.Helper()
	return tr.locations(tr.group(name).Definition(tr.after(name, before, sub)))
}

func (tr *tree) referencesAfter(name, before, sub string) []string {
	tr.t.Helper()
	g := tr.group(name)
	sym, ok := g.At(tr.after(name, before, sub))
	if !ok {
		tr.t.Fatalf("no symbol at %q after %q in %s", sub, before, name)
	}
	return tr.describe(References(g, sym, true))
}

func (tr *tree) renamedAfter(name, before, sub, newName string) (map[string]string, error) {
	tr.t.Helper()
	g := tr.group(name)
	sym, ok := g.At(tr.after(name, before, sub))
	if !ok {
		tr.t.Fatalf("no symbol at %q after %q in %s", sub, before, name)
	}
	occs, err := g.Rename(sym, newName)
	if err != nil {
		return nil, err
	}
	edits := map[string][]textedit.Edit{}
	for _, o := range occs {
		rel, err := filepath.Rel(tr.dir, o.File.Path)
		if err != nil {
			tr.t.Fatal(err)
		}
		edits[filepath.ToSlash(rel)] = append(edits[filepath.ToSlash(rel)], o.Edits(newName)...)
	}
	out := map[string]string{}
	for file, list := range edits {
		out[file] = textedit.Apply(tr.files[file], list)
	}
	return out, nil
}

func expectRename(t *testing.T, got map[string]string, err error, want map[string]string) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(got, want) {
		t.Errorf("renamed to\n%q\nwant\n%q", got, want)
	}
}

var importTree = map[string]string{
	"default.nix":     "let\n  viaLet = import ./lib.nix;\n  dirValue = import ./dir;\n  path = ./lib.nix;\n  viaPath = import path;\nin [\n  (import ./lib.nix).answer\n  viaLet.answer\n  dirValue.nested\n  viaPath.answer\n  (import ./fn.nix { one = 1; }).two\n]\n",
	"lib.nix":         "{\n  answer = 42;\n}\n",
	"dir/default.nix": "{\n  nested = 1;\n}\n",
	"fn.nix":          "{ one }: {\n  two = one;\n}\n",
}

func TestNixDefinitionFollowsImports(t *testing.T) {
	t.Parallel()

	tr := newTree(t, importTree)
	g := tr.group("default.nix")
	for _, tt := range []struct {
		sub  string
		nth  int
		want string
	}{
		{"answer", 0, "lib.nix:2:answer"},
		{"answer", 1, "lib.nix:2:answer"},
		{"answer", 2, "lib.nix:2:answer"},
		{"nested", 0, "dir/default.nix:2:nested"},
		{"two", 0, "fn.nix:2:two"},
		{"one", 0, "fn.nix:1:one"},
		{"./lib.nix", 0, "lib.nix:1:"},
		{"./dir", 0, "dir/default.nix:1:"},
		{"./fn.nix", 0, "fn.nix:1:"},
	} {
		if got := tr.locations(g.Definition(tr.offset("default.nix", tt.sub, tt.nth))); !slices.Equal(got, []string{tt.want}) {
			t.Errorf("definition of %q #%d = %v, want %s", tt.sub, tt.nth, got, tt.want)
		}
	}
}

func TestNixReferencesComeFromImporters(t *testing.T) {
	t.Parallel()

	tr := newTree(t, importTree)
	g, sym := tr.at("lib.nix", "answer", 0)
	expectStrings(t, "references of an imported attribute", tr.describe(References(g, sym, true)), []string{
		"default.nix:7:answer",
		"default.nix:8:answer",
		"default.nix:10:answer",
		"lib.nix:2:answer",
	})
	g, sym = tr.at("fn.nix", "one", 0)
	expectStrings(t, "references of a formal", tr.describe(References(g, sym, true)), []string{
		"default.nix:11:one",
		"fn.nix:1:one",
		"fn.nix:2:one",
	})
}

func TestNixRenameAcrossFiles(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		files map[string]string
		in    string
		sub   string
		want  map[string]string
	}{
		{
			name:  "a formal updates the keys of its callPackage callers",
			files: map[string]string{"default.nix": "{ callPackage }: callPackage ./y.nix { foo = 1; }\n", "y.nix": "{ foo }: foo\n"},
			in:    "y.nix",
			sub:   "foo",
			want:  map[string]string{"default.nix": "{ callPackage }: callPackage ./y.nix { alpha = 1; }\n", "y.nix": "{ alpha }: alpha\n"},
		},
		{
			name:  "from the key",
			files: map[string]string{"default.nix": "{ pkgs }: pkgs.callPackage ./y.nix { foo = 1; }\n", "y.nix": "{ foo }: foo\n"},
			in:    "default.nix",
			sub:   "foo",
			want:  map[string]string{"default.nix": "{ pkgs }: pkgs.callPackage ./y.nix { alpha = 1; }\n", "y.nix": "{ alpha }: alpha\n"},
		},
		{
			name:  "an inherited key keeps what it reads",
			files: map[string]string{"default.nix": "{ lib, foo }: lib.callPackageWith { } ./y.nix { inherit foo; }\n", "y.nix": "{ foo }: foo\n"},
			in:    "y.nix",
			sub:   "foo",
			want:  map[string]string{"default.nix": "{ lib, foo }: lib.callPackageWith { } ./y.nix { alpha = foo; }\n", "y.nix": "{ alpha }: alpha\n"},
		},
		{
			name:  "an import called with a set",
			files: map[string]string{"default.nix": "import ./y.nix { foo = 1; }\n", "y.nix": "{ foo }: foo\n"},
			in:    "y.nix",
			sub:   "foo",
			want:  map[string]string{"default.nix": "import ./y.nix { alpha = 1; }\n", "y.nix": "{ alpha }: alpha\n"},
		},
		{
			name:  "an attribute every importer selects",
			files: map[string]string{"default.nix": "(import ./y.nix).foo\n", "y.nix": "{\n  foo = 1;\n}\n"},
			in:    "y.nix",
			sub:   "foo",
			want:  map[string]string{"default.nix": "(import ./y.nix).alpha\n", "y.nix": "{\n  alpha = 1;\n}\n"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := newTree(t, tt.files).renamedAfter(tt.in, "", tt.sub, "alpha")
			expectRename(t, got, err, tt.want)
		})
	}
}

const (
	deferredFlake  = "{\n  outputs = inputs@{ flake-parts, ... }: flake-parts.lib.mkFlake { inherit inputs; } { imports = [ ./y.nix ]; };\n}\n"
	deferredModule = "{ config, inputs, lib, ... }: {\n  options.deferred = lib.mkOption { };\n  config.deferred.sub.foo = 1;\n  config.flake.nixosConfigurations.a = inputs.nixpkgs.lib.nixosSystem {\n    modules = [\n      config.deferred\n      { options.sub.foo = lib.mkOption { }; }\n    ];\n  };\n"
)

func TestNixRenameThroughAModulePassedToTheModuleSystem(t *testing.T) {
	t.Parallel()

	got, err := newTree(t, map[string]string{"flake.nix": deferredFlake, "y.nix": deferredModule + "}\n"}).renamedAfter("y.nix", "config.deferred.sub.", "foo", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got["y.nix"], "config.deferred.sub.alpha = 1;") || !strings.Contains(got["y.nix"], "options.sub.alpha = lib.mkOption") {
		t.Errorf("renamed to %q", got["y.nix"])
	}
}

func TestNixRenameRefusesWhatOtherFilesCannotFollow(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		files map[string]string
		in    string
		sub   string
	}{
		{name: "callPackage fills a formal by name", files: map[string]string{"default.nix": "{ callPackage }: callPackage ./y.nix { }\n", "y.nix": "{ foo }: foo\n"}, in: "y.nix", sub: "foo"},
		{name: "the set leaves through an import", files: map[string]string{"default.nix": "{ f }: f (import ./y.nix)\n", "y.nix": "{\n  foo = 1;\n}\n"}, in: "y.nix", sub: "foo"},
		{name: "a selected set is passed on", files: map[string]string{"default.nix": "{ f }: f (import ./y.nix).bar\n", "y.nix": "{\n  bar.foo = 1;\n}\n"}, in: "y.nix", sub: "foo"},
		{name: "a set below a module is passed on", files: map[string]string{"flake.nix": deferredFlake, "y.nix": deferredModule + "  config.copy = f config.deferred.sub;\n}\n"}, in: "y.nix", sub: "foo"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := newTree(t, tt.files).renamedAfter(tt.in, "", tt.sub, "alpha"); !errors.Is(err, navigation.ErrNotRenamable) {
				t.Errorf("error = %v, want %v", err, navigation.ErrNotRenamable)
			}
		})
	}
	tr := newTree(t, map[string]string{"default.nix": "(import ./y.nix).foo + (import ./y.nix).bar\n", "y.nix": "{\n  foo = 1;\n}\n"})
	if _, err := tr.renamedAfter("y.nix", "", "foo", "bar"); !errors.Is(err, navigation.ErrNameConflict) {
		t.Errorf("renaming onto a name another file reads: %v, want %v", err, navigation.ErrNameConflict)
	}
}

func TestNixImportCyclesTerminate(t *testing.T) {
	t.Parallel()

	tr := newTree(t, map[string]string{
		"a.nix": "let\n  b = import ./b.nix;\nin {\n  one = b.two;\n}\n",
		"b.nix": "let\n  a = import ./a.nix;\nin {\n  two = a.one;\n}\n",
		"c.nix": "import ./d.nix\n",
		"d.nix": "import ./c.nix\n",
		"e.nix": "(import ./c.nix).nothing\n",
		"f.nix": "{ imports = [ ./g.nix ]; }\n",
		"g.nix": "{ imports = [ ./f.nix ]; }\n",
	})
	expectStrings(t, "a read around the cycle", tr.definitionAfter("a.nix", "b.", "two"), []string{"b.nix:4:two"})
	expectStrings(t, "and back", tr.definitionAfter("b.nix", "a.", "one"), []string{"a.nix:4:one"})
	if got := tr.definitionAfter("e.nix", ").", "nothing"); len(got) != 0 {
		t.Errorf("a value that is only a cycle has %v", got)
	}
	if got := tr.definitionAfter("f.nix", "[ ", "./g.nix"); !slices.Equal(got, []string{"g.nix:1:"}) {
		t.Errorf("a path in a cycle of modules = %v", got)
	}
}

func TestNixFlakesStaySeparate(t *testing.T) {
	t.Parallel()

	flake := "{\n  inputs.nixpkgs.url = \"github:NixOS/nixpkgs\";\n  outputs = { self, nixpkgs }: {\n    value = (import ./lib.nix).shared;\n    copy = self.value;\n    system = nixpkgs.lib;\n  };\n}\n"
	lib := "{\n  shared = 1;\n}\n"
	tr := newTree(t, map[string]string{
		"one/flake.nix": flake, "one/lib.nix": lib,
		"two/flake.nix": flake, "two/lib.nix": lib,
		"flake.nix": "{\n  outputs = _: { };\n}\n",
	})
	g, sym := tr.at("one/lib.nix", "shared", 0)
	expectStrings(t, "references stay inside the flake", tr.describe(References(g, sym, true)), []string{"one/flake.nix:4:shared", "one/lib.nix:2:shared"})
	expectStrings(t, "definition stays inside the flake", tr.definitionAfter("two/flake.nix", ").", "shared"), []string{"two/lib.nix:2:shared"})
	expectStrings(t, "an output through self", tr.definitionAfter("two/flake.nix", "self.", "value"), []string{"two/flake.nix:4:value"})
	expectStrings(t, "a formal goes to its input", tr.definitionAfter("one/flake.nix", "{ self, ", "nixpkgs"), []string{"one/flake.nix:2:nixpkgs"})
	expectStrings(t, "a read of the formal too", tr.definitionAfter("one/flake.nix", "system = ", "nixpkgs"), []string{"one/flake.nix:2:nixpkgs"})
}

func TestNixDiagnosticsNeverTouchTheProject(t *testing.T) {
	t.Parallel()

	tr := newTree(t, importTree)
	file, err := tr.ws.Load(tr.path("default.nix"))
	if err != nil {
		t.Fatal(err)
	}
	graphs := 0
	env := Env{Loader: tr.ws}
	env.Graph = func(ctx context.Context) (*Graph, error) {
		graphs++
		return NewGraph(ctx, host.Nix, tr.all, env)
	}
	g, err := Build(context.Background(), hostOf(t, file), env)
	if err != nil {
		t.Fatal(err)
	}
	g.Findings()
	if graphs != 0 || len(g.Module.Files) != 1 {
		t.Errorf("diagnostics built the graph %d times and hold %d files", graphs, len(g.Module.Files))
	}
	if _, ok := g.At(tr.offset("default.nix", "answer", 0)); !ok || graphs != 1 {
		t.Errorf("navigation found nothing or built the graph %d times", graphs)
	}
}

func TestNixModuleNamedFlakeIsNotAFlake(t *testing.T) {
	t.Parallel()

	tr := newTree(t, map[string]string{
		"flake.nix":             "{\n  inputs.nixpkgs.url = \"github:NixOS/nixpkgs\";\n  outputs = inputs@{ flake-parts, ... }: flake-parts.lib.mkFlake { inherit inputs; } (inputs.import-tree ./modules);\n}\n",
		"modules/flake.nix":     "{ inputs, ... }: {\n  shared = inputs.nixpkgs;\n}\n",
		"modules/set/flake.nix": "{\n  config.greeting = 1;\n}\n",
		"modules/read.nix":      "{ config, ... }: {\n  copy = config.greeting;\n}\n",
	})
	expectStrings(t, "a function named flake.nix", tr.definitionAfter("modules/flake.nix", "= inputs.", "nixpkgs"), []string{tr.site("flake.nix", "  inputs.", "nixpkgs")})
	expectStrings(t, "a set named flake.nix", tr.referencesAfter("modules/set/flake.nix", "config.", "greeting"), []string{
		tr.site("modules/read.nix", "config.", "greeting"),
		tr.site("modules/set/flake.nix", "config.", "greeting"),
	})
}

func TestNixSingleFlakeCorpus(t *testing.T) {
	t.Parallel()

	tr := corpusTree(t, "single-flake")
	input := tr.site("flake.nix", "  inputs.", "nixpkgs")
	expectStrings(t, "self reaches the outputs", tr.definitionAfter("flake.nix", "[ self.", "overlays"), []string{tr.site("flake.nix", "    {\n      ", "overlays")})
	expectStrings(t, "the nixpkgs formal", tr.definitionAfter("flake.nix", "self,\n      ", "nixpkgs"), []string{input})
	expectStrings(t, "the inputs alias", tr.definitionAfter("flake.nix", "revision = inputs.", "nixpkgs"), []string{input})
	expectStrings(t, "an imported let", tr.definitionAfter("flake.nix", "release.defaults.", "retentionDays"), []string{tr.site("release.nix", "    ", "retentionDays")})
	expectStrings(t, "a value passed to callPackage", tr.definitionAfter("pkgs/logship.nix", "inherit (release) ", "version"), []string{tr.site("release.nix", "  ", "version")})
	expectStrings(t, "a key of callPackage", tr.definitionAfter("flake.nix", "./pkgs/rotate.nix {\n          ", "retentionDays"), []string{tr.site("pkgs/rotate.nix", "  ", "retentionDays")})
	got, err := tr.renamedAfter("pkgs/rotate.nix", "  ", "retentionDays", "keepDays")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got["flake.nix"], "keepDays = release.defaults.retentionDays;") || !strings.Contains(got["pkgs/rotate.nix"], "toString keepDays") {
		t.Errorf("renamed to %q", got)
	}
}

func TestNixFlakePartsCorpus(t *testing.T) {
	t.Parallel()

	tr := corpusTree(t, "flake-parts")
	const packages = "nix/packages/default.nix"
	const manual = "nix/packages/manual.nix"
	const project = "nix/project.nix"
	const checks = "nix/checks.nix"
	option := tr.site(project, "    project.", "version")
	setting := tr.site(project, "config.project.", "version")
	mirrorctl := tr.site(packages, "        ", "mirrorctl")
	input := tr.site("flake.nix", "    ", "nixpkgs")
	expectStrings(t, "an option and its setting", tr.definitionAfter(checks, "config.project.", "version"), []string{option, setting})
	expectStrings(t, "a perSystem option", tr.definitionAfter("nix/shell.nix", "config.project.", "syncTools"), []string{tr.site(project, "options.project.", "syncTools")})
	expectStrings(t, "self' reaches perSystem", tr.definitionAfter("nix/shell.nix", "self'.packages.", "mirrorctl"), []string{mirrorctl})
	expectStrings(t, "a path in imports", tr.definitionAfter("flake.nix", "./nix/project.nix\n        ", "./nix/packages"), []string{packages + ":1:"})
	expectStrings(t, "inherit from self'", tr.definitionAfter(checks, "inherit (self'.packages) mirrorctl ", "manual"), []string{tr.site(manual, "packages.", "manual")})
	expectStrings(t, "withSystem", tr.definitionAfter("nix/overlay.nix", "config.packages.", "mirrorctl"), []string{mirrorctl})
	expectStrings(t, "inputs in a module", tr.definitionAfter("nix/overlay.nix", "inputs.", "nixpkgs"), []string{input})
	expectStrings(t, "config in the flake", tr.definitionAfter("nix/overlay.nix", "config.flake.overlays.", "default"), []string{tr.site("nix/overlay.nix", "    overlays.", "default")})
	expectStrings(t, "a follows goes to the input", tr.definitionAfter("flake.nix", "follows = \"", "nixpkgs"), []string{input})
	expectStrings(t, "a callPackage key", tr.definitionAfter(packages, "{\n          ", "version"), []string{tr.site("pkgs/mirrorctl.nix", "  ", "version")})
	expectStrings(t, "every reader of an option", tr.referencesAfter(project, "project.", "version"), []string{
		tr.site(checks, "\"$reported\" != \"${config.project.", "version"),
		tr.site(checks, "expected ${config.project.", "version"),
		tr.site(packages, "config.project.", "version"),
		tr.site(manual, "config.project.", "version"),
		option,
		setting,
	})
	got, err := tr.renamedAfter(project, "options.project.", "syncTools", "tools")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got["nix/shell.nix"], "config.project.tools") || !strings.Contains(got[packages], "syncTools = config.project.tools;") || !strings.Contains(got[project], "options.project.tools") {
		t.Errorf("renamed to %q", got)
	}
}

func TestNixDendriticCorpus(t *testing.T) {
	t.Parallel()

	tr := corpusTree(t, "dendritic")
	const (
		box            = "modules/hosts/box.nix"
		configurations = "modules/core/configurations.nix"
		coreFlake      = "modules/core/flake.nix"
		editor         = "modules/editor.nix"
		features       = "modules/core/features.nix"
		images         = "modules/images.nix"
		laptop         = "modules/hosts/laptop.nix"
		owner          = "modules/core/lib/owner.nix"
		paths          = "modules/core/paths.nix"
		tools          = "modules/tools.nix"
	)
	setting := tr.site(owner, "harbor.lib.", "ownerName")
	nixpkgsFor := tr.site(coreFlake, "_module.args.", "nixpkgsFor")
	expectStrings(t, "a setting found through import-tree", tr.definitionAfter(editor, "config.harbor.lib.", "ownerName"), []string{setting})
	expectStrings(t, "a read from its setting", tr.referencesAfter(owner, "harbor.lib.", "ownerName"), []string{
		setting,
		tr.site(editor, "config.harbor.lib.", "ownerName"),
	})
	expectStrings(t, "a merged option", tr.definitionAfter(editor, "lib.mkIf config.features.", "editor"), []string{
		tr.site(features, "flake.modules.homeManager.features = { lib, ... }: {\n    options.features.", "editor"),
		tr.site(box, "homeModules;\n        features.", "editor"),
		tr.site(laptop, "features.", "editor"),
	})
	expectStrings(t, "a class of its own", tr.definitionAfter(editor, "systemPackages = lib.mkIf config.features.", "editor"), []string{
		tr.site(features, "flake.modules.nixos.features = { lib, ... }: {\n    options.features.", "editor"),
		tr.site(box, "hostName = \"box\";\n      features.", "editor"),
	})
	expectStrings(t, "an option from the home module", tr.definitionAfter(editor, "config.harbor.paths.", "stateDir"), []string{
		tr.site(paths, "options.harbor.paths.", "stateDir"),
		tr.site(paths, "config.harbor.paths.", "stateDir"),
		tr.site(laptop, "harbor.paths.", "stateDir"),
	})
	expectStrings(t, "a deferred module", tr.definitionAfter(images, "config.flake.modules.nixos.", "editor"), []string{tr.site(editor, "flake.modules.nixos.", "editor")})
	expectStrings(t, "every class set under one name", tr.definitionAfter(box, "config.flake.modules.", "nixos"), []string{
		tr.site(features, "flake.modules.", "nixos"),
		tr.site(editor, "flake.modules.", "nixos"),
	})
	expectStrings(t, "a formal from _module.args", tr.definitionAfter(images, "= ", "nixpkgsFor"), []string{nixpkgsFor})
	expectStrings(t, "the formal itself", tr.definitionAfter(configurations, "withSystem,\n  ", "nixpkgsFor"), []string{nixpkgsFor})
	expectStrings(t, "a formal and its binding are one", tr.referencesAfter(coreFlake, "_module.args.", "nixpkgsFor"), []string{
		tr.site(configurations, "withSystem,\n  ", "nixpkgsFor"),
		tr.site(configurations, "pkgs = ", "nixpkgsFor"),
		nixpkgsFor,
		tr.site(images, "config,\n  ", "nixpkgsFor"),
		tr.site(images, "= ", "nixpkgsFor"),
	})
	expectStrings(t, "pkgs from perSystem _module.args", tr.definitionAfter(tools, "  ", "pkgs"), []string{tr.site(coreFlake, "_module.args.", "pkgs")})
	expectStrings(t, "inputs in a module", tr.definitionAfter(coreFlake, "import inputs.", "nixpkgs"), []string{tr.site("flake.nix", "    ", "nixpkgs")})
	expectStrings(t, "self' in a module", tr.definitionAfter(tools, "self'.packages.", "greet"), []string{tr.site(tools, "      packages.", "greet")})
	expectStrings(t, "a key to an excluded file", tr.definitionAfter(tools, "{\n        ", "greeting"), []string{tr.site("modules/_pkgs/greet.nix", "  ", "greeting")})
	expectStrings(t, "the custom host tree", tr.definitionAfter(box, "${config.harbor.", "owner"), []string{
		tr.site(features, "options.harbor.", "owner"),
		tr.site(box, "      harbor.", "owner"),
	})
	g, sym := tr.at("modules/_disabled/old-owner.nix", "ownerName", 0)
	expectStrings(t, "a file import-tree skips is alone", tr.describe(References(g, sym, true)), []string{"modules/_disabled/old-owner.nix:2:ownerName"})

	got, err := tr.renamedAfter("modules/core/paths.nix", "options.harbor.paths.", "stateDir", "stateRoot")
	if err != nil {
		t.Fatal(err)
	}
	for file, want := range map[string]string{
		"modules/core/paths.nix":   "config.harbor.paths.stateRoot = lib.mkDefault",
		"modules/editor.nix":       "EDITOR_STATE = config.harbor.paths.stateRoot;",
		"modules/hosts/laptop.nix": "harbor.paths.stateRoot = \"/var/lib/harbor\";",
	} {
		if !strings.Contains(got[file], want) {
			t.Errorf("%s renamed to %q", file, got[file])
		}
	}
	got, err = tr.renamedAfter("modules/images.nix", "= ", "nixpkgsFor", "pkgsOf")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got["modules/core/flake.nix"], "_module.args.pkgsOf = nixpkgsFor;") || !strings.Contains(got["modules/core/configurations.nix"], "pkgs = pkgsOf host.system;") {
		t.Errorf("renamed to %q", got)
	}
	for _, tt := range []struct{ file, before, sub string }{
		{"modules/core/lib/owner.nix", "harbor.lib.", "ownerName"},
		{"modules/core/flake.nix", "_module.args.", "pkgs"},
		{"modules/hosts/box.nix", "networking.", "hostName"},
		{"modules/tools.nix", "  ", "pkgs"},
	} {
		if _, err := tr.renamedAfter(tt.file, tt.before, tt.sub, "alpha"); !errors.Is(err, navigation.ErrNotRenamable) {
			t.Errorf("renaming %s in %s: %v, want %v", tt.sub, tt.file, err, navigation.ErrNotRenamable)
		}
	}
}
