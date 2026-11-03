package nix

import (
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func refactoring(t *testing.T, src, marker string, cursor bool, title string) (Refactoring, *Document, bool) {
	t.Helper()
	at := strings.Index(src, marker)
	if at < 0 {
		t.Fatalf("%q is not in %q", marker, src)
	}
	selection := model.Span{Start: at, End: at + len(marker)}
	if cursor {
		selection.End = at
	}
	d := Read(src)
	i := slices.IndexFunc(d.Refactorings(selection), func(r Refactoring) bool { return strings.HasPrefix(r.Title, title) })
	if i < 0 {
		return Refactoring{}, d, false
	}
	return d.Refactorings(selection)[i], d, true
}

func TestRefactoringsKeepTheFileAndItsMeanings(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, title, src, marker string
		cursor                   bool
		want                     string
	}{
		{"extract into the let around it", "Extract", "let\n  a = 1;\nin\n  a + 2 * 3\n", "2 * 3", false, "let\n  a = 1;\n  extracted = 2 * 3;\nin\n  a + extracted\n"},
		{"extract in place of a lambda body", "Extract", "x: x + 1\n", "x + 1", false, "x: let extracted = x + 1; in extracted\n"},
		{"extract stays inside the lambda it reads", "Extract", "let\n  a = 1;\nin\n  x: x + a\n", "x + a", false, "let\n  a = 1;\nin\n  x: let extracted = x + a; in extracted\n"},
		{"extract picks a name the file does not hold", "Extract", "let\n  extracted = 1;\nin\n  extracted + (2 + 3)\n", "2 + 3", false, "let\n  extracted = 1;\n  extracted2 = 2 + 3;\nin\n  extracted + (extracted2)\n"},
		{"extract from a list", "Extract", "[ (1 + 2) ]\n", "1 + 2", false, "[ (let extracted = 1 + 2; in extracted) ]\n"},
		{"extract what reads through with", "Extract", "with p; { a = [ q ]; }\n", "[ q ]", false, "with p; { a = let extracted = [ q ]; in extracted; }\n"},
		{"inline the only binding", "Inline", "let\n  a = 1 + 2;\nin\n  a * 3\n", "a", true, "(1 + 2) * 3\n"},
		{"inline from its use", "Inline", "let\n  a = 1 + 2;\nin\n  3 * a\n", "a\n", true, "3 * (1 + 2)\n"},
		{"inline one binding of several", "Inline", "let\n  a = 1;\n  b = a;\nin\n  b + b\n", "a", true, "let\n  b = 1;\nin\n  b + b\n"},
		{"inline keeps a selection apart", "Inline", "let\n  a = b.c;\n  b = { c = { }; };\nin\n  a.d or 1\n", "a", true, "let\n  b = { c = { }; };\nin\n  (b.c).d or 1\n"},
		{"inherit a variable", "Convert", "y: { y = y; }\n", "y = y", true, "y: { inherit y; }\n"},
		{"inherit from a set", "Convert", "a: { x = a.x; }\n", "x = a.x", true, "a: { inherit (a) x; }\n"},
		{"inherit from a deeper set", "Convert", "a: { x = a.b.x; }\n", "x = a.b.x", true, "a: { inherit (a.b) x; }\n"},
		{"nest a path", "Nest", "{ a.b.c = 1; }\n", "a.b.c", true, "{ a = { b.c = 1; }; }\n"},
		{"nest under rec", "Nest", "rec { a.b = 1; c = a; }\n", "a.b", true, "rec { a = { b = 1; }; c = a; }\n"},
		{"flatten a set", "Flatten", "{\n  a = {\n    b = 1;\n    c.d = 2;\n  };\n}\n", "a =", true, "{\n  a.b = 1;\n  a.c.d = 2;\n}\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, d, ok := refactoring(t, tt.src, tt.marker, tt.cursor, tt.title)
			if !ok {
				t.Fatalf("%s was not offered", tt.title)
			}
			got := textedit.Apply(tt.src, r.Edits)
			if got != tt.want {
				t.Errorf("result = %q, want %q", got, tt.want)
			}
			after := syntax.Parse(got)
			if len(after.Errors) > 0 {
				t.Fatalf("the result does not parse: %+v\n%s", after.Errors, got)
			}
			table := scope.Resolve(after)
			before, now := meanings(d.tree, d.table, r.gone, ""), meanings(after, table, nil, r.fresh)
			if !slices.Equal(before, now) {
				t.Errorf("meanings changed\nbefore %q\nafter  %q", before, now)
			}
			for _, u := range table.Uses() {
				if u.Ref.Kind == scope.Undefined && !slices.ContainsFunc(d.table.Uses(), func(o scope.Use) bool { return o.Name == u.Name && o.Ref.Kind == scope.Undefined }) {
					t.Errorf("%s became undefined in %q", u.Name, got)
				}
			}
		})
	}
}

func TestRefactoringsRefuseToChangeMeaning(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, title, src, marker string
		cursor                   bool
	}{
		{"extract half an operator chain", "Extract", "a: a + 1 + 2\n", "1 + 2", false},
		{"extract a bare name", "Extract", "a: [ a ]\n", "a ]", false},
		{"extract an attribute name", "Extract", "{ \"a\" = 1; }\n", "\"a\"", false},
		{"extract the parentheses of an inherit", "Extract", "a: { inherit (a) b; }\n", "(a)", false},
		{"extract a position", "Extract", "{ a = [ __curPos ]; }\n", "[ __curPos ]", false},
		{"extract from a file that does not parse", "Extract", "{ a = [ 1 ]; b = ; }\n", "[ 1 ]", false},
		{"extract across a comment", "Extract", "[ (1 + # c\n 2) ]\n", "1 + # c\n 2", false},
		{"inline a capture by a lambda", "Inline", "let\n  y = 0;\n  a = y;\nin\n  y: a\n", "a =", true},
		{"inline a capture by a let", "Inline", "let\n  y = 0;\n  a = y;\nin\n  let y = 1; in a + y\n", "a =", true},
		{"inline into another with", "Inline", "with p;\nlet\n  a = q;\nin\n  with r; a\n", "a =", true},
		{"inline with two reads", "Inline", "let a = 1; in a + a\n", "a =", true},
		{"inline itself", "Inline", "let a = x: a x; in 1\n", "a =", true},
		{"inline a rec attribute", "Inline", "rec { a = 1; b = a; }\n", "a =", true},
		{"inline a position", "Inline", "let a = __curPos; in a\n", "a =", true},
		{"inline an inherit", "Inline", "x: let inherit (x) a; in a\n", "a; in", true},
		{"inline away a comment", "Inline", "let\n  a = 1 + # why\n    2;\n  b = 2;\nin\n  a + b + b\n", "a =", true},
		{"inherit reads itself in a let", "Convert", "let x = x; in x\n", "x =", true},
		{"inherit reads itself in rec", "Convert", "rec { x = x; }\n", "x =", true},
		{"inherit a selection with a default", "Convert", "a: { x = a.x or 1; }\n", "x =", true},
		{"inherit another name", "Convert", "a: { x = a.y; }\n", "x =", true},
		{"nest beside another path of the same head", "Nest", "{ a.b = 1; a.c = 2; }\n", "a.b", true},
		{"flatten rec", "Flatten", "{ a = rec { b = 1; }; }\n", "a =", true},
		{"flatten an inherit", "Flatten", "x: { a = { inherit x; }; }\n", "a =", true},
		{"flatten nothing", "Flatten", "{ a = { }; }\n", "a =", true},
		{"flatten beside another path of the same head", "Flatten", "{ a = { b = 1; }; a.c = 2; }\n", "a =", true},
		{"flatten across a comment", "Flatten", "{ a = { # c\n  b = 1; }; }\n", "a =", true},
		{"flatten a dynamic key", "Flatten", "x: { a = { ${x} = 1; }; }\n", "a =", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if r, _, ok := refactoring(t, tt.src, tt.marker, tt.cursor, tt.title); ok {
				t.Errorf("offered %q, giving %q", r.Title, textedit.Apply(tt.src, r.Edits))
			}
		})
	}
}

func TestRefactoringsFromTheOtherServers(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, title, src, marker string
		want                     string
	}{
		{"quoted to indented on one line", "Rewrite as an indented", `x: "a ${x} b"` + "\n", "a ", "x: ''a ${x} b''\n"},
		{"quoted with a line break", "Rewrite as an indented", `{ a = "one\ntwo\n"; }` + "\n", "one", "{ a = ''\n  one\n  two\n''; }\n"},
		{"quoted with no final line break", "Rewrite as an indented", `{ a = "one\ntwo"; }` + "\n", "one", "{ a = ''\n  one\n  two''; }\n"},
		{"quoted keeps an escaped antiquotation", "Rewrite as an indented", `"\${x}"` + "\n", "x", "''''${x}''\n"},
		{"quoted keeps quotes and tabs", "Rewrite as an indented", `"\"a\"\tb"` + "\n", "a", "''\"a\"''\\tb''\n"},
		{"indented to quoted", "Rewrite as a quoted", "{\n  a = ''\n    one\n      two\n  '';\n}\n", "one", "{\n  a = \"one\\n  two\\n\";\n}\n"},
		{"indented with an antiquotation", "Rewrite as a quoted", "x: ''\n  a ${x}\n''\n", "a ", "x: \"a ${x}\\n\"\n"},
		{"indented with characters a quoted string escapes", "Rewrite as a quoted", "''\n  \"a\" \\b ''${c}\n''\n", "a", "\"\\\"a\\\" \\\\b \\${c}\\n\"\n"},
		{"inherit to a binding", "Convert to `a = a;`", "a: { inherit a; }\n", "inherit", "a: { a = a; }\n"},
		{"inherit from a set", "Convert this inherit", "{ inherit (s) a b; }\n", "inherit", "{ a = s.a; b = s.b; }\n"},
		{"inherit on its own line", "Convert this inherit", "{\n  inherit (s.t) a b;\n}\n", "inherit", "{\n  a = s.t.a;\n  b = s.t.b;\n}\n"},
		{"inherit from an expression", "Convert to", "{ inherit (f x) a; }\n", "inherit", "{ a = (f x).a; }\n"},
		{"quote an attribute", "Quote `a`", "{ a = 1; }\n", "a", "{ \"a\" = 1; }\n"},
		{"unquote an attribute", "Remove the quotes", "{ \"a\" = 1; }\n", "a", "{ a = 1; }\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, d, ok := refactoring(t, tt.src, tt.marker, true, tt.title)
			if !ok {
				var titles []string
				for _, r := range d.Refactorings(model.Span{Start: strings.Index(tt.src, tt.marker), End: strings.Index(tt.src, tt.marker)}) {
					titles = append(titles, r.Title)
				}
				t.Fatalf("no %q among %q", tt.title, titles)
			}
			if got := textedit.Apply(tt.src, r.Edits); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRefactoringsFromTheOtherServersKeepTheValue(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, title, src, marker string }{
		{"an indented string that would lose its indentation", "Rewrite as an indented", `"  a"` + "\n", "a"},
		{"a keyword cannot lose its quotes", "Remove the quotes", "{ \"if\" = 1; }\n", "if"},
		{"a name with a dash stays quoted when it must", "Remove the quotes", "{ \"a b\" = 1; }\n", "a b"},
		{"inherit in a let would read itself", "Convert to", "a: let inherit a; in a\n", "inherit"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if r, _, ok := refactoring(t, tt.src, tt.marker, true, tt.title); ok {
				t.Errorf("offered %q: %+v", r.Title, r.Edits)
			}
		})
	}
}

func TestWithBecomesLetInherit(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src, marker, want string
	}{
		{"the names read through it", "{ pkgs }: with pkgs; [ hello jq hello ]", "with", "{ pkgs }: let inherit (pkgs) hello jq; in [ hello jq hello ]"},
		{"a comment stays", "{ pkgs }: with pkgs; # tools\n[ hello ]", "pkgs;", "{ pkgs }: let inherit (pkgs) hello; in # tools\n[ hello ]"},
		{"a scope that is not a name", "{ a }: with a.b; [ c ]", "with", "{ a }: let inherit (a.b) c; in [ c ]"},
		{"lexical names are left alone", "{ pkgs, x }: with pkgs; [ x y ]", "with", "{ pkgs, x }: let inherit (pkgs) y; in [ x y ]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, _, ok := refactoring(t, tt.src, tt.marker, true, "Convert `with`")
			if !ok {
				t.Fatal("no refactoring was offered")
			}
			if got := textedit.Apply(tt.src, r.Edits); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
	for _, tt := range []struct {
		name, src, marker string
	}{
		{"an outer with could answer", "{ a, b }: with a; with b; [ c ]", "with b"},
		{"an inner with may answer", "{ a, b }: with a; [ (with b; c) d ]", "with a"},
		{"nothing is read through it", "{ a, x }: with a; [ x ]", "with"},
		{"the cursor is in the body", "{ a }: with a; [ c ]", "[ c"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if r, _, ok := refactoring(t, tt.src, tt.marker, true, "Convert `with`"); ok {
				t.Errorf("offered: %+v", r)
			}
		})
	}
}

func TestExtractingABindingToAFile(t *testing.T) {
	t.Parallel()

	src := "{ x, lib }:\nlet\n  y = 1;\nin\n{\n  cfg = { a = x; b = y; c = lib.id; d = (z: z) 1; };\n}\n"
	r, _, ok := refactoring(t, src, "cfg", true, "Extract to")
	if !ok {
		t.Fatal("no refactoring was offered")
	}
	if want := "{ x, lib }:\nlet\n  y = 1;\nin\n{\n  cfg = import ./cfg.nix { inherit lib x y; };\n}\n"; textedit.Apply(src, r.Edits) != want {
		t.Errorf("host = %q", textedit.Apply(src, r.Edits))
	}
	if r.Create == nil || r.Create.Name != "cfg.nix" || r.Create.Text != "{\n  lib,\n  x,\n  y,\n}:\n{\n  a = x;\n  b = y;\n  c = lib.id;\n  d = (z: z) 1;\n}\n" {
		t.Errorf("new file = %+v", r.Create)
	}
	for _, tt := range []struct {
		name, src, marker string
	}{
		{"it reads through a with", "{ p }: with p; { cfg = { a = b; }; }", "cfg"},
		{"it reads an unknown name", "{ cfg = { a = b; }; }", "cfg"},
		{"it reads itself", "rec { cfg = { a = cfg; }; }", "cfg ="},
		{"a value not worth a file", "{ cfg = 1; }", "cfg"},
		{"its position would change", "{ cfg = { a = __curPos; }; }", "cfg"},
		{"a name a path cannot spell", "{ \"foo bar\" = { a = 1; }; }", "foo bar"},
		{"a name with a directory", "{ \"a/b\" = { a = 1; }; }", "a/b"},
		{"a name that leaves the directory", "{ \"../up\" = { a = 1; }; }", "../up"},
		{"a name with an apostrophe", "{ cfg' = { a = 1; }; }", "cfg'"},
		{"a name that is empty", "{ \"\" = { a = 1; }; }", "\"\""},
		{"a name that would hide the file", "{ \".a\" = { a = 1; }; }", ".a"},
		{"a name that would read a variable", "{ \"a\\${b}\" = { a = 1; }; }", "a\\$"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if r, _, ok := refactoring(t, tt.src, tt.marker, true, "Extract to"); ok {
				t.Errorf("offered: %+v", r)
			}
		})
	}
}

func TestExtractingKeepsTheLayoutOfTheNewFile(t *testing.T) {
	t.Parallel()

	src := "{ x }:\n{\n  a = {\n    cfg = {\n      b = x;\n      c = \"one\ntwo\";\n      d = ''\n        three\n\n          four\n      '';\n    };\n  };\n}\n"
	r, _, ok := refactoring(t, src, "cfg", true, "Extract to")
	if !ok {
		t.Fatal("no refactoring was offered")
	}
	if want := "{ x }:\n{\n  b = x;\n  c = \"one\ntwo\";\n  d = ''\n    three\n\n      four\n  '';\n}\n"; r.Create == nil || r.Create.Text != want {
		t.Errorf("new file = %+v, want %q", r.Create, want)
	}
}

func TestAnOptionsTypeIsFoundAtItsDeclaration(t *testing.T) {
	t.Parallel()

	src := "{ lib, ... }:\n{\n  options.a = lib.mkOption { type = lib.types.port; default = 1; };\n  options.b = lib.mkEnableOption \"b\";\n  options.c = lib.mkOption { default = 1; };\n  d = 1;\n}\n"
	d := Read(src)
	if got, ok := d.OptionTypeAt(strings.Index(src, "options.a")); !ok || src[got.Start:got.End] != "lib.types.port" {
		t.Errorf("type of a = %+v, %v", got, ok)
	}
	for _, name := range []string{"options.b", "options.c", "d ="} {
		if got, ok := d.OptionTypeAt(strings.Index(src, name)); ok {
			t.Errorf("type of %s = %+v", name, got)
		}
	}
}
