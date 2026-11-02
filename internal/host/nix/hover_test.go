package nix

import (
	"strings"
	"testing"
)

func TestHoverOnBuiltinsAndKeywords(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src, covers string
		want              []string
	}{
		{name: "a global builtin", src: "ma|p", covers: "map", want: []string{"```nix\nmap f list\n```", "Takes 2 arguments."}},
		{name: "the end of a name", src: "map|", covers: "map", want: []string{"map f list"}},
		{name: "one argument", src: "toSt|ring", covers: "toString", want: []string{"toString e", "Takes 1 argument."}},
		{name: "a constant", src: "tr|ue", covers: "true", want: []string{"The Boolean value true.", "A value, not a function."}},
		{name: "an alias", src: "__a|dd", covers: "__add", want: []string{"__add e1 e2", "Alias of `builtins.add`."}},
		{name: "builtins itself", src: "buil|tins.map", covers: "builtins", want: []string{"every built-in function"}},
		{name: "a member", src: "builtins.ad|d", covers: "add", want: []string{"add e1 e2", "Adds two numbers."}},
		{name: "a member through a binding", src: "let b = builtins; in b.ad|d", covers: "add", want: []string{"add e1 e2"}},
		{name: "a has-attr test", src: "builtins ? ad|d", covers: "add", want: []string{"add e1 e2"}},
		{name: "an inherit from builtins", src: "{ inherit (builtins) ad|d; }", covers: "add", want: []string{"add e1 e2"}},
		{name: "an inherited global", src: "{ inherit ma|p; }", covers: "map", want: []string{"map f list"}},
		{name: "through with", src: "with builtins; ad|d", covers: "add", want: []string{"add e1 e2"}},
		{name: "let", src: "l|et a = 1; in a", covers: "let", want: []string{"```nix\nlet\n```", "mutually recursive"}},
		{name: "in", src: "let a = 1; i|n a", covers: "in", want: []string{"Ends the bindings"}},
		{name: "or", src: "a: a.b o|r 1", covers: "or", want: []string{"fallback"}},
		{name: "inherit", src: "{ inh|erit a; }", covers: "inherit", want: []string{"Binds names"}},
		{name: "rec", src: "r|ec { }", covers: "rec", want: []string{"refer to one another"}},
		{name: "then", src: "if true th|en 1 else 2", covers: "then", want: []string{"is true"}},
		{name: "after non-ASCII", src: "[ \"é😀\" ma|p ]", covers: "map", want: []string{"map f list"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			text, at := marked(t, tt.src)
			got, ok := Read(text).Hover(at)
			if !ok {
				t.Fatalf("%q has no hover", tt.src)
			}
			if span := text[got.Span.Start:got.Span.End]; span != tt.covers {
				t.Errorf("%q covers %q, want %q", tt.src, span, tt.covers)
			}
			for _, want := range tt.want {
				if !strings.Contains(got.Markdown, want) {
					t.Errorf("%q lacks %q:\n%s", tt.src, want, got.Markdown)
				}
			}
		})
	}
}

func TestHoverLeavesBindingsAlone(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		"let ma|p = 1; in map",
		"let map = 1; in ma|p",
		"map: ma|p",
		"let s = { add = 1; }; in s.ad|d",
		"builtins.no|pe",
		"builtins.__a|dd",
		"p: with p; ad|d",
		"\"ma|p\"",
		"# ma|p",
		"[ | ]",
	} {
		text, at := marked(t, src)
		if got, ok := Read(text).Hover(at); ok {
			t.Errorf("%q hovers %q", src, got.Markdown)
		}
	}
}

func TestCommentAboveADefinition(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src, want string
	}{
		{"a line comment", "let\n  # Doc.\n  |a = 1;\nin a", "Doc."},
		{"several lines", "let\n  # One.\n  #   Two.\n  |a = 1;\nin a", "One.\nTwo."},
		{"a block comment", "let\n  /* Doc. */\n  |a = 1;\nin a", "Doc."},
		{"a path", "let\n  # Doc.\n  s.|a = 1;\nin s", "Doc."},
		{"inherit", "x: let\n  # Doc.\n  inherit |x;\nin x", "Doc."},
		{"a formal", "{\n  # Doc.\n  |a,\n}: a", "Doc."},
		{"the innermost binding", "{\n  # Outer.\n  a = {\n    # Inner.\n    |b = 1;\n  };\n}", "Inner."},
		{"a blank line apart", "let\n  # Doc.\n\n  |a = 1;\nin a", ""},
		{"the end of the line before", "let\n  b = 1; # About b.\n  |a = 1;\nin a + b", ""},
		{"a directive is no doc", "let\n  # shellcheck disable=SL8005\n  |a = 1;\nin 2", ""},
		{"a directive under a doc", "let\n  # Doc.\n  # shellcheck disable=SL8005\n  |a = 1;\nin 2", "Doc."},
		{"nothing above", "let |a = 1; in a", ""},
		{"non-ASCII", "let\n  # é😀\n  |a = 1;\nin a", "é😀"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			text, at := marked(t, tt.src)
			if got := Read(text).CommentAbove(at); got != tt.want {
				t.Errorf("%q = %q, want %q", tt.src, got, tt.want)
			}
		})
	}
}
