package hostnav

import (
	"errors"
	"slices"
	"strings"
	"testing"

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
