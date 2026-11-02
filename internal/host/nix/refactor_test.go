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
