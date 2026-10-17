package justfile

import (
	"slices"
	"testing"
)

func TestConstant(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, text string
		want       []string
		ok         bool
	}{
		{"string", `x := "a"`, []string{"a"}, true},
		{"escapes", `x := "\u{41}\t"`, []string{"A\t"}, true},
		{"raw string", `x := '\n'`, []string{`\n`}, true},
		{"concatenation", `x := "a" + 'b'`, []string{"ab"}, true},
		{"join", `x := "a" / "b"`, []string{"a/b"}, true},
		{"root", `x := / "a"`, []string{"/a"}, true},
		{"group", `x := ("a")`, []string{"a"}, true},
		{"variable", "y := \"z\"\nx := y", []string{"z"}, true},
		{"later variable", "y := x\nx := \"z\"\nw := y", []string{"z"}, true},
		{"if", `x := if "a" == "a" { "y" } else { "n" }`, []string{"y"}, true},
		{"else", `x := if "a" != "a" { "y" } else { "n" }`, []string{"n"}, true},
		{"if without else", `x := if "a" == "b" { "y" }`, nil, true},
		{"regex", `x := if "abc" =~ "^a" { "y" } else { "n" }`, []string{"y"}, true},
		{"format string", "y := \"b\"\nx := f\"a{{ y }}c{{{{\"", []string{"abc{{"}, true},
		{"indented format string", "x := f'''\n  a{{\"b\"}}\n  '''", []string{"ab\n"}, true},
		{"list", `x := ["a", "b" + "c"]`, []string{"a", "bc"}, true},
		{"function call", `x := env("A", "b")`, nil, false},
		{"backtick", "x := `echo`", nil, false},
		{"shell-expanded string", `x := x"~/a"`, nil, false},
		{"self reference", "x := x", nil, false},
		{"undefined", "x := y", nil, false},
		{"invalid regex", `x := if "a" =~ "(" { "y" } else { "n" }`, nil, false},
	} {
		f := Parse(tt.text)
		got, ok := f.Constant(f.Assignments()[len(f.Assignments())-1].Value)
		if ok != tt.ok || !slices.Equal(got, tt.want) {
			t.Errorf("%s: Constant = %q, %v, want %q, %v", tt.name, got, ok, tt.want, tt.ok)
		}
	}
}
