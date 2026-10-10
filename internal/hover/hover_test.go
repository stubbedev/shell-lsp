package hover

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

type fakeCommands map[string]string

func (f fakeCommands) Describe(_ context.Context, name string) (string, error) {
	doc, ok := f[name]
	if !ok {
		return "", errors.New("no such command " + name)
	}
	return doc, nil
}

const script = `#!/bin/bash
# Greets someone.
greet() {
	local -a names=("$1" "$2")
	printf '%s\n' "${names[@]}" "${#names}" "${name:-anon}" "$@"
}
count=0
export LOG_DIR=/tmp
for f in *.txt; do
	greet "$f" "$count"
	(( count += 1 ))
done
if [[ -n $HOME ]]; then ls "$LOG_DIR"; fi
echo "$BASH_SOURCE $UNSET_THING $1"
`

func hoverAt(t *testing.T, src string, d dialect.Dialect, needle string, nth int) Result {
	t.Helper()

	offset := -1
	for range nth + 1 {
		next := strings.Index(src[offset+1:], needle)
		if next < 0 {
			t.Fatalf("needle %q #%d not found", needle, nth)
		}
		offset += next + 1
	}
	got, ok, err := Hover(t.Context(), Input{
		Tree:     parser.Parse(src, d),
		Offset:   offset,
		Commands: fakeCommands{"ls": "**ls - list directory contents**"},
	})
	if err != nil {
		t.Fatalf("Hover(%q) error = %v", needle, err)
	}
	if !ok {
		t.Fatalf("Hover(%q) found nothing", needle)
	}
	return got
}

func TestHover(t *testing.T) {
	t.Parallel()

	tests := []struct {
		needle string
		nth    int
		want   []string
		span   string
	}{
		{needle: "greet()", want: []string{"greet $1 $2 [args...]", "Greets someone.", "line 3"}, span: "greet"},
		{needle: "greet \"$f\"", want: []string{"greet $1 $2 [args...]"}, span: "greet"},
		{needle: "local", want: []string{"local [option] name", "**bash:**"}, span: "local"},
		{needle: "names[@]", want: []string{"local array in `greet`", "Declared on line 4", "local -a names"}, span: "${names[@]}"},
		{needle: "#names", want: []string{"`${#name}`: length"}, span: "${#names}"},
		{needle: "name:-", want: []string{"not assigned in this file", "`${name:-word}`"}, span: "${name:-anon}"},
		{needle: "$1\"", want: []string{"argument 1 of function `greet`"}, span: "$1"},
		{needle: "$@", want: []string{"All positional parameters"}, span: "$@"},
		{needle: "count=0", want: []string{"global variable", "Assigned in 2 places"}, span: "count"},
		{needle: "count +=", want: []string{"global variable"}, span: "count"},
		{needle: "LOG_DIR=", want: []string{"exported global variable"}, span: "LOG_DIR"},
		{needle: "f in", want: []string{"(loop variable)"}, span: "f"},
		{needle: "for", want: []string{"for name [in words ...]", "**bash:**"}, span: "for"},
		{needle: "done", want: []string{"Ends the body of a loop"}, span: "done"},
		{needle: "[[", want: []string{"Conditional expression"}, span: "[["},
		{needle: "HOME", want: []string{"Home directory"}, span: "$HOME"},
		{needle: "ls ", want: []string{"list directory contents"}, span: "ls"},
		{needle: "BASH_SOURCE", want: []string{"source file names"}, span: "$BASH_SOURCE"},
		{needle: "$1", nth: 1, want: []string{"positional parameter 1 of the script"}, span: "$1"},
	}
	for _, tc := range tests {
		got := hoverAt(t, script, dialect.Bash, tc.needle, tc.nth)
		for _, want := range tc.want {
			if !strings.Contains(got.Markdown, want) {
				t.Errorf("hover %q: markdown lacks %q:\n%s", tc.needle, want, got.Markdown)
			}
		}
		if span := script[got.Span.Start:got.Span.End]; span != tc.span {
			t.Errorf("hover %q: span = %q, want %q", tc.needle, span, tc.span)
		}
	}
}

func TestHoverDialectNotes(t *testing.T) {
	t.Parallel()

	got := hoverAt(t, "local x=1\n", dialect.Posix, "local", 0)
	if !strings.Contains(got.Markdown, "_Not available in sh._") {
		t.Errorf("posix local hover = %q", got.Markdown)
	}
	got = hoverAt(t, "echo -n hi\n", dialect.Dash, "echo", 0)
	if !strings.Contains(got.Markdown, "**dash:**") {
		t.Errorf("dash echo hover = %q", got.Markdown)
	}
}

func TestHoverZshFlagsAndModifiers(t *testing.T) {
	t.Parallel()

	src := "print -r -- ${(j:,:)parts} ${(U)name} ${file:t:r}\n"
	got := hoverAt(t, src, dialect.Zsh, "j:,:", 0)
	if !strings.Contains(got.Markdown, "- `j`: join") || strings.Contains(got.Markdown, "`,`") {
		t.Errorf("flags hover = %q", got.Markdown)
	}
	got = hoverAt(t, src, dialect.Zsh, "(U)", 0)
	if !strings.Contains(got.Markdown, "- `U`: convert to uppercase") {
		t.Errorf("U flag hover = %q", got.Markdown)
	}
	got = hoverAt(t, src, dialect.Zsh, "file:t", 0)
	if !strings.Contains(got.Markdown, "- `:t`: tail") || !strings.Contains(got.Markdown, "- `:r`: root") {
		t.Errorf("modifier hover = %q", got.Markdown)
	}
}

func TestHoverShadowingPrefersLocals(t *testing.T) {
	t.Parallel()

	src := "x=global\nf() {\n\tlocal x=inner\n\techo \"$x\"\n}\necho \"$x\"\n"
	inner := hoverAt(t, src, dialect.Bash, "$x", 0)
	if !strings.Contains(inner.Markdown, "local variable in `f`") {
		t.Errorf("inner hover = %q", inner.Markdown)
	}
	outer := hoverAt(t, src, dialect.Bash, "$x", 1)
	if !strings.Contains(outer.Markdown, "global variable") || !strings.Contains(outer.Markdown, "line 1") {
		t.Errorf("outer hover = %q", outer.Markdown)
	}
}

func TestHoverCommandLookupError(t *testing.T) {
	t.Parallel()

	_, _, err := Hover(t.Context(), Input{
		Tree:     parser.Parse("unknown-tool --flag\n", dialect.Bash),
		Offset:   2,
		Commands: fakeCommands{},
	})
	if err == nil {
		t.Error("Hover() swallowed the command lookup error")
	}
}

func TestHoverNothing(t *testing.T) {
	t.Parallel()

	_, ok, err := Hover(t.Context(), Input{Tree: parser.Parse("echo hi   \n", dialect.Bash), Offset: 8})
	if err != nil || ok {
		t.Errorf("Hover(whitespace) = %t, %v; want nothing", ok, err)
	}
}

func TestFunctionSignature(t *testing.T) {
	t.Parallel()

	src := "# Copies a file.\n# $1 - source path\n# dest: where to copy\ncopy() {\n\tlocal src=\"$1\" dest=${2:-.}\n\tshift 2\n\tcp \"$src\" \"$dest\" \"$@\"\n}\n"
	tree := parser.Parse(src, dialect.Bash)
	sig := FunctionSignature(tree, tree.Functions[0])
	if sig.Label() != "copy src dest [args...]" || sig.Doc != "Copies a file." {
		t.Errorf("signature = %q, doc %q", sig.Label(), sig.Doc)
	}
	if len(sig.Params) != 2 || sig.Params[0].Doc != "source path" || sig.Params[1].Doc != "where to copy" {
		t.Errorf("params = %+v", sig.Params)
	}
}
