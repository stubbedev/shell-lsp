package parser

import (
	"testing"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

func TestFunctionDefinitionInAList(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		"f() { return 0; } && complete -F f x\n",
		"f() ( true ) || echo failed\n",
		"f() { :; } && g && h\n",
	} {
		tree := Parse(src, dialect.Bash)
		list, ok := tree.File.Stmts[0].Cmd.(*syntax.BinaryCmd)
		if !ok {
			t.Fatalf("%q: top-level command is %T, want the and/or list", src, tree.File.Stmts[0].Cmd)
		}
		for inner, ok := list.X.Cmd.(*syntax.BinaryCmd); ok; inner, ok = inner.X.Cmd.(*syntax.BinaryCmd) {
			list = inner
		}
		decl, ok := list.X.Cmd.(*syntax.FuncDecl)
		if !ok {
			t.Fatalf("%q: leftmost command is %T, want the function", src, list.X.Cmd)
		}
		if _, isList := decl.Body.Cmd.(*syntax.BinaryCmd); isList {
			t.Errorf("%q: function body still contains the list", src)
		}
		if fn, ok := tree.FunctionNamed("f"); !ok || fn.Decl != decl {
			t.Errorf("%q: index does not point at the lifted function", src)
		}
	}
}
