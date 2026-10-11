package parser

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

func describeFlags(text string, flags []ZshFlag) []string {
	out := make([]string, 0, len(flags))
	for _, f := range flags {
		var sb strings.Builder
		sb.WriteByte(f.Name)
		for _, arg := range f.Args {
			if text[arg.Span.Start:arg.Span.End] != arg.Value {
				return []string{"span of " + arg.Value + " is " + text[arg.Span.Start:arg.Span.End]}
			}
			fmt.Fprintf(&sb, "<%s>", arg.Value)
		}
		out = append(out, sb.String())
	}
	return out
}

func firstParamExp(t *testing.T, tree *Tree) *syntax.ParamExp {
	t.Helper()
	var found *syntax.ParamExp
	Walk(tree.File, func(n syntax.Node) bool {
		if pe, ok := n.(*syntax.ParamExp); ok && found == nil {
			found = pe
		}
		return found == nil
	})
	if found == nil {
		t.Fatal("no parameter expansion")
	}
	return found
}

func TestZshParameterFlags(t *testing.T) {
	t.Parallel()

	for src, want := range map[string][]string{
		"print ${(U)x}\n":               {"U"},
		"print ${(j:, :)x}\n":           {"j<, >"},
		"print ${(l:n::.:)x}\n":         {"l<n><.>"},
		"print ${(r:12:: :)x}\n":        {"r<12>< >"},
		"print ${(l[w][0][-]qs.x.)x}\n": {"l<w><0><->", "q", "s<x>"},
		"print ${(pj.$sep.)x}\n":        {"p", "j<$sep>"},
		"print ${(I:n:s{/}%)x}\n":       {"I<n>", "s</>", "%"},
	} {
		tree := Parse(src, dialect.Zsh)
		pe := firstParamExp(t, tree)
		if pe.Flags == nil {
			t.Errorf("%q: no flags", src)
			continue
		}
		got := describeFlags(src, ParameterFlags(pe.Flags))
		if !slices.Equal(got, want) {
			t.Errorf("%q: flags = %v, want %v", src, got, want)
		}
	}
}

func TestZshSubscriptPatterns(t *testing.T) {
	t.Parallel()

	for src, want := range map[string]bool{
		"${a[(r)x*]}\n":            true,
		"${a[(Ie)x]}\n":            true,
		"${h[(K)x]}\n":             true,
		"${a[(n:2:i)x]}\n":         true,
		"${s[(w)2]}\n":             false,
		"${s[(s:r:w)2]}\n":         false,
		"${a[(b:i:w)2]}\n":         false,
		"print $(( [#16] 255 ))\n": false,
	} {
		tree := Parse(src, dialect.Zsh)
		var flags *syntax.FlagsArithm
		Walk(tree.File, func(n syntax.Node) bool {
			if fa, ok := n.(*syntax.FlagsArithm); ok {
				flags = fa
			}
			return true
		})
		if flags == nil {
			t.Errorf("%q: no subscript flags", src)
			continue
		}
		if got := SubscriptPattern(flags); got != want {
			t.Errorf("%q: SubscriptPattern = %v, want %v", src, got, want)
		}
		if got := IsArithmetic(flags); got == want {
			t.Errorf("%q: IsArithmetic = %v, want %v", src, got, !want)
		}
	}
}

func TestZshSubscriptPatternsAreNotReferences(t *testing.T) {
	t.Parallel()

	tree := Parse("print ${words[(r)beta]} ${words[(w)gamma]} ${history[(R)${(b)prefix}*]}\n", dialect.Zsh)
	var refs []string
	for _, r := range tree.References {
		refs = append(refs, r.Name)
	}
	if want := []string{"words", "words", "gamma", "history", "prefix"}; !slices.Equal(refs, want) {
		t.Errorf("references = %v, want %v", refs, want)
	}
}

func TestZshReadsInFlagsAndOperands(t *testing.T) {
	t.Parallel()

	src := "print ${(l:w+1::.:)x} ${(ps.$sep.j.$glue.)x} ${a[(n:k:r)p]} ${a:^b} ${a:|c} ${(j.$no.)x}\nreturn r*2\nrepeat times :\n"
	tree := Parse(src, dialect.Zsh)
	if len(tree.Errors) != 0 {
		t.Fatalf("errors = %v, want none", messages(tree))
	}
	var refs []string
	for _, r := range tree.References {
		refs = append(refs, src[r.Span.Start:r.Span.End])
	}
	want := []string{"x", "w", "x", "sep", "glue", "a", "k", "a", "b", "a", "c", "x", "r", "times"}
	if !slices.Equal(refs, want) {
		t.Errorf("references = %v, want %v", refs, want)
	}
}

func TestStatusIsArithmeticOnlyInZsh(t *testing.T) {
	t.Parallel()

	for d, want := range map[dialect.Dialect][]string{dialect.Zsh: {"ret"}, dialect.Bash: nil} {
		tree := Parse("f() { return ret; }\n", d)
		var refs []string
		for _, r := range tree.References {
			refs = append(refs, r.Name)
		}
		if !slices.Equal(refs, want) {
			t.Errorf("%s: references = %v, want %v", d, refs, want)
		}
	}
}
