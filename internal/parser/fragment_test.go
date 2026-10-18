package parser

import (
	"errors"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const fragmentMark = "MARK"

func markLit(t *testing.T, root syntax.Node) *syntax.Lit {
	t.Helper()

	var found *syntax.Lit
	Walk(root, func(n syntax.Node) bool {
		lits := []*syntax.Lit{}
		switch n := n.(type) {
		case *syntax.Lit:
			lits = append(lits, n)
		case *syntax.ParamExp:
			lits = append(lits, n.Modifiers...)
		}
		for _, lit := range lits {
			if strings.Contains(lit.Value, fragmentMark) {
				found = lit
			}
		}
		return found == nil
	})
	return found
}

func TestParseFragmentClosesWhatIsOpenAtItsEnd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		d        dialect.Dialect
		text     string
		parent   func(syntax.Node) bool
		describe string
	}{
		{d: dialect.Bash, text: "[[ -f a && -", describe: "a test operand", parent: func(n syntax.Node) bool { _, ok := n.(*syntax.TestClause); return ok }},
		{d: dialect.Bash, text: "[[ \"$", describe: "a parameter in quotes in a test", parent: func(n syntax.Node) bool { _, ok := n.(*syntax.TestClause); return ok }},
		{d: dialect.Bash, text: "echo ${arr[@]:", describe: "a slice offset", parent: func(n syntax.Node) bool { pe, ok := n.(*syntax.ParamExp); return ok && pe.Slice != nil }},
		{d: dialect.Bash, text: "echo ${m[", describe: "a subscript", parent: func(n syntax.Node) bool { pe, ok := n.(*syntax.ParamExp); return ok && pe.Index != nil }},
		{d: dialect.Bash, text: "if [[ $(echo \"${x:-", describe: "an operand nested in a test", parent: func(n syntax.Node) bool { _, ok := n.(*syntax.IfClause); return ok }},
		{d: dialect.Zsh, text: "print ${(j", describe: "parameter flags", parent: func(n syntax.Node) bool { pe, ok := n.(*syntax.ParamExp); return ok && pe.Flags != nil }},
		{d: dialect.Zsh, text: "print -l *(", describe: "glob qualifiers", parent: func(n syntax.Node) bool { _, ok := n.(*syntax.CallExpr); return ok }},
		{d: dialect.Zsh, text: "print ${list:", describe: "a modifier", parent: func(n syntax.Node) bool { pe, ok := n.(*syntax.ParamExp); return ok && len(pe.Modifiers) == 1 }},
		{d: dialect.Bash, text: "f() {\n\tif true; then\n\t\techo '", describe: "a single-quoted word in a body", parent: func(n syntax.Node) bool { _, ok := n.(*syntax.FuncDecl); return ok }},
	}
	for _, tc := range tests {
		text := "x=1\n" + tc.text
		tree := Parse(text, tc.d)
		start := len("x=1\n")
		file, err := tree.ParseFragment(Span{Start: start, End: len(text)}, fragmentMark)
		if err != nil {
			t.Errorf("%s: ParseFragment(%q) error = %v", tc.describe, tc.text, err)
			continue
		}
		if len(file.Stmts) == 0 || Offset(file.Stmts[0].Pos()) != start {
			t.Errorf("%s: the fragment does not start at offset %d of the text", tc.describe, start)
		}
		if !Any(file, tc.parent) {
			t.Errorf("%s: the closed fragment lacks the expected node", tc.describe)
		}
		var sq *syntax.SglQuoted
		Walk(file, func(n syntax.Node) bool {
			if q, ok := n.(*syntax.SglQuoted); ok {
				sq = q
			}
			return true
		})
		lit := markLit(t, file)
		switch {
		case sq != nil:
			if !strings.HasPrefix(sq.Value, fragmentMark) {
				t.Errorf("%s: single-quoted value = %q, want it to start with the suffix", tc.describe, sq.Value)
			}
		case lit == nil:
			t.Errorf("%s: no literal holds the suffix", tc.describe)
		case Offset(lit.End()) < len(text)+len(fragmentMark):
			t.Errorf("%s: the literal holding the suffix ends at %d, before the suffix", tc.describe, Offset(lit.End()))
		}
	}
}

func TestParseFragmentReportsWhatItCannotClose(t *testing.T) {
	t.Parallel()

	text := "[[ $a "
	tree := Parse(text, dialect.Bash)
	_, err := tree.ParseFragment(Span{Start: 0, End: len(text)}, fragmentMark)
	parseErr, ok := errors.AsType[syntax.ParseError](err)
	if !ok || Offset(parseErr.Pos) != len(text) {
		t.Fatalf("ParseFragment(%q) error = %v, want a parse error at the suffix", text, err)
	}
	if _, err := tree.ParseFragment(Span{Start: 0, End: len(text)}, ""); err != nil {
		t.Errorf("ParseFragment(%q) without a suffix error = %v, want the test closed", text, err)
	}
}

func TestParseFragmentBlanksAByteOrderMark(t *testing.T) {
	t.Parallel()

	text := string(byteOrderMark) + "echo "
	tree := Parse(text, dialect.Bash)
	file, err := tree.ParseFragment(Span{Start: 0, End: len(text)}, fragmentMark)
	if err != nil {
		t.Fatal(err)
	}
	call, ok := file.Stmts[0].Cmd.(*syntax.CallExpr)
	if !ok || !LiteralIs(call.Args[0], "echo") || len(call.Args) != 2 {
		t.Errorf("fragment after a byte order mark = %#v", file.Stmts[0].Cmd)
	}
}

func TestParseHeredocFragment(t *testing.T) {
	t.Parallel()

	text := "cat <<EOF\nhello ${name:-"
	tree := Parse(text, dialect.Bash)
	start := strings.Index(text, "hello")
	word, err := tree.ParseHeredocFragment(Span{Start: start, End: len(text)}, fragmentMark)
	if err != nil {
		t.Fatal(err)
	}
	if Offset(word.Pos()) != start || len(word.Parts) != 2 {
		t.Fatalf("heredoc fragment = %d parts at %d", len(word.Parts), Offset(word.Pos()))
	}
	pe, ok := word.Parts[1].(*syntax.ParamExp)
	if !ok || pe.Exp == nil || pe.Exp.Op != syntax.DefaultUnsetOrNull || !strings.HasPrefix(pe.Exp.Word.Lit(), fragmentMark) {
		t.Errorf("heredoc expansion = %#v", word.Parts[1])
	}
}
