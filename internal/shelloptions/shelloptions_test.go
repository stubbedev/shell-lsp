package shelloptions

import (
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func TestScan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		dialect dialect.Dialect
		src     string
		want    Options
	}{
		{dialect.Bash, "#!/bin/bash -e\nset -o pipefail\nshopt -s extglob globstar nocasematch\n", Options{Errexit: true, Pipefail: true, ExtGlob: true, GlobStar: true, NoCaseMatch: true}},
		{dialect.Bash, "shopt -u patsub_replacement\nshopt -s nullglob\n", Options{NoPatsubAmp: true, NullGlob: true}},
		{dialect.Zsh, "setopt extended_glob KSH_GLOB\nunsetopt case_glob\n", Options{ExtendedGlob: true, KshGlob: true, NoCaseGlob: true}},
		{dialect.Zsh, "f() { emulate -L zsh -o extendedglob -o nullglob; }\n", Options{ExtendedGlob: true, NullGlob: true}},
		{dialect.Zsh, "emulate ksh\n", Options{GlobSubst: true, ShWordSplit: true, KshGlob: true, NoBareGlobQual: true}},
		{dialect.Zsh, "emulate -R sh -c 'true'\n", Options{GlobSubst: true, ShWordSplit: true, NoBareGlobQual: true}},
		{dialect.Zsh, "#compdef foo\n_foo() { :; }\n", Options{ExtendedGlob: true}},
		{dialect.Bash, "#compdef foo\n", Options{}},
		{dialect.Zsh, "f() { builtin emulate -L zsh -o EXTENDED_GLOB; }\n", Options{ExtendedGlob: true}},
		{dialect.Bash, "command shopt -s extglob\n", Options{ExtGlob: true}},
		{dialect.Zsh, "opts=(extendedglob)\nsetopt $opts\n", Options{Dynamic: true}},
	}
	for _, tt := range tests {
		if got := Scan(parser.Parse(tt.src, tt.dialect)).At(len(tt.src)); got != tt.want {
			t.Errorf("Scan(%q) = %+v, want %+v", tt.src, got, tt.want)
		}
	}
}

func TestEnabledBy(t *testing.T) {
	t.Parallel()

	tree := parser.Parse("shopt -s extglob\nshopt -u extglob\nf() { shopt -s nullglob extglob; }\n", dialect.Bash)
	got := Scan(tree).EnabledBy("extglob", len(tree.Text()))
	if len(got) != 2 || got[0].Span.Start != 0 || tree.Line(got[1].Span.Start) != 3 {
		t.Fatalf("EnabledBy(extglob) = %+v", got)
	}
}

func TestOptionsStayInTheirShell(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		src  string
		at   string
		want Options
	}{
		{"(shopt -s nullglob)\necho out\n", "out", Options{}},
		{"(shopt -s nullglob; echo in)\n", "in", Options{NullGlob: true}},
		{"shopt -s nullglob\n(echo in)\n", "in", Options{NullGlob: true}},
		{"shopt -s nullglob\n(shopt -s extglob; (echo in))\n", "in", Options{NullGlob: true, ExtGlob: true}},
		{"(shopt -s nullglob)\n(echo in)\n", "in", Options{}},
		{"x=$(set -o pipefail)\necho out\n", "out", Options{}},
		{"shopt -s extglob | cat\necho out\n", "out", Options{}},
		{"(opts=x; shopt -s $opts)\necho out\n", "out", Options{}},
		{"(opts=x; shopt -s $opts; echo in)\n", "in", Options{Dynamic: true}},
		{"f() { shopt -s nullglob; }\necho out\n", "out", Options{NullGlob: true}},
	} {
		tree := parser.Parse(tt.src, dialect.Bash)
		if got := Scan(tree).At(strings.Index(tt.src, tt.at)); got != tt.want {
			t.Errorf("%q: options at %q = %+v, want %+v", tt.src, tt.at, got, tt.want)
		}
	}
}

func TestEnabledByOnlyNamesTheShellsThatRunHere(t *testing.T) {
	t.Parallel()

	src := "(shopt -s extglob)\nshopt -s extglob\n(echo in)\n"
	tree := parser.Parse(src, dialect.Bash)
	got := Scan(tree).EnabledBy("extglob", strings.Index(src, "in"))
	if len(got) != 1 || tree.Line(got[0].Span.Start) != 2 {
		t.Fatalf("EnabledBy(extglob) = %+v, want only the shopt on line 2", got)
	}
}
