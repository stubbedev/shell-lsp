package lint

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func findingsWith(t *testing.T, src string, code Code, optional ...string) []Finding {
	t.Helper()
	var out []Finding
	for _, f := range Check(parser.Parse(src, dialect.Bash), nil, Options{Optional: optional}) {
		if f.Code == code {
			out = append(out, f)
		}
	}
	return out
}

func TestRequireVariableBracesFix(t *testing.T) {
	t.Parallel()

	src := "name=x\necho \"$name\" $1 $name.txt\n"
	got := findingsWith(t, src, 2250, "require-variable-braces")
	if len(got) != 2 {
		t.Fatalf("SC2250 findings = %+v, want two", got)
	}
	fixed := src
	for _, f := range slices.Backward(got) {
		fixed = textedit.Apply(fixed, fixEdits(f))
	}
	if want := "name=x\necho \"${name}\" $1 ${name}.txt\n"; fixed != want {
		t.Errorf("fixed = %q, want %q", fixed, want)
	}
	if off := findingsWith(t, src, 2250); len(off) != 0 {
		t.Errorf("SC2250 reported without being enabled: %+v", off)
	}
}

func TestRequireVariableBracesNamesZshForms(t *testing.T) {
	t.Parallel()

	src := "print -r -- $#BUFFER $fg[red] $+commands[git]\n"
	var messages []string
	fixed := src
	findings := Check(parser.Parse(src, dialect.Zsh), nil, Options{Optional: []string{"require-variable-braces"}})
	for _, f := range slices.Backward(findings) {
		if f.Code == 2250 {
			messages = append(messages, f.Message)
			fixed = textedit.Apply(fixed, fixEdits(f))
		}
	}
	want := []string{
		"Write ${+commands[git]} instead of $+commands[git] to make the end of the variable name explicit.",
		"Write ${fg[red]} instead of $fg[red] to make the end of the variable name explicit.",
		"Write ${#BUFFER} instead of $#BUFFER to make the end of the variable name explicit.",
	}
	if !slices.Equal(messages, want) {
		t.Errorf("messages = %q, want %q", messages, want)
	}
	if want := "print -r -- ${#BUFFER} ${fg[red]} ${+commands[git]}\n"; fixed != want {
		t.Errorf("fixed = %q, want %q", fixed, want)
	}
}

func TestScanBracedClassifiesFirstPart(t *testing.T) {
	t.Parallel()

	cases := []struct {
		src   string
		kinds []bracePartKind
		plain bool
	}{
		{"${name}", []bracePartKind{braceLiteral}, false},
		{"${${x}%.*}", []bracePartKind{braceNested, braceLiteral}, true},
		{"${${x%.*}#a}", []bracePartKind{braceNested, braceLiteral}, false},
		{"${$(a ')' b)//x/y}", []bracePartKind{braceCommand, braceLiteral}, false},
		{"${$((1 + (2)))}", []bracePartKind{braceArithmetic}, false},
		{"${\"a b\"}", []bracePartKind{braceDouble}, false},
		{"${a\\}b}", []bracePartKind{braceLiteral}, false},
		{"${@(a|b)}", []bracePartKind{braceExtGlob}, false},
		{"${x$y}", []bracePartKind{braceLiteral, braceVariable}, false},
	}
	for _, c := range cases {
		body, end, ok := scanBraced(c.src, 0)
		if !ok || end != len(c.src) {
			t.Errorf("scanBraced(%q) = end %d, ok %t", c.src, end, ok)
			continue
		}
		kinds := make([]bracePartKind, len(body))
		for i, part := range body {
			kinds[i] = part.kind
		}
		if !slices.Equal(kinds, c.kinds) {
			t.Errorf("scanBraced(%q) kinds = %v, want %v", c.src, kinds, c.kinds)
		}
		if body[0].plain != c.plain {
			t.Errorf("scanBraced(%q) plain = %t, want %t", c.src, body[0].plain, c.plain)
		}
	}
	if _, _, ok := scanBraced("${unterminated", 0); ok {
		t.Error("an unterminated expansion was accepted")
	}
}
