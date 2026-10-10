package lint

import (
	"slices"
	"testing"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func calls(t *testing.T, src string) []*syntax.CallExpr {
	t.Helper()
	var out []*syntax.CallExpr
	syntax.Walk(parser.Parse(src, dialect.Bash).File, func(n syntax.Node) bool {
		if c, ok := n.(*syntax.CallExpr); ok {
			out = append(out, c)
		}
		return true
	})
	if len(out) == 0 {
		t.Fatalf("no simple command in %q", src)
	}
	return out
}

func firstCall(t *testing.T, src string) *syntax.CallExpr { return calls(t, src)[0] }

func TestCommandOf(t *testing.T) {
	t.Parallel()

	c, ok := commandOf(firstCall(t, "builtin read -r line\n"))
	if !ok || c.name != "read" || !c.builtin || len(c.args) != 2 || !c.is("read") || c.isBase("read") {
		t.Errorf("builtin read = %+v, %t", c, ok)
	}
	c, ok = commandOf(firstCall(t, "/usr/bin/grep -q x\n"))
	if !ok || c.is("grep") || !c.isBase("grep") {
		t.Errorf("path-qualified grep = %+v, %t", c, ok)
	}
	if _, ok := commandOf(firstCall(t, "$tool --help\n")); ok {
		t.Error("an expanded command name was treated as literal")
	}
	args, ok := c.literalArgs()
	if !ok || !slices.Equal(args, []string{"-q", "x"}) {
		t.Errorf("literalArgs = %v, %t", args, ok)
	}
}

func TestParseFlags(t *testing.T) {
	t.Parallel()

	f := parseFlags(firstCall(t, "read -rp 'Name: ' -t5 --silent name -- -literal\n").Args[1:], "pt")
	for _, flag := range []byte("rpt") {
		if !f.has(flag, "") {
			t.Errorf("flag -%c missing", flag)
		}
	}
	if !f.has(0, "silent") || f.has('s', "") {
		t.Errorf("long flags = %v, short = %v", f.long, f.short)
	}
	var operands []string
	for _, w := range f.operands {
		lit, _ := parser.Literal(w)
		operands = append(operands, lit)
	}
	if !slices.Equal(operands, []string{"name", "-literal"}) {
		t.Errorf("operands = %v", operands)
	}
}

func TestIsGlob(t *testing.T) {
	t.Parallel()

	for src, want := range map[string]bool{
		"ls *.txt\n":                    true,
		"ls file?\n":                    true,
		"ls [ab]\n":                     true,
		"ls \\*.txt\n":                  false,
		"ls '*.txt'\n":                  false,
		"ls a[$i]\n":                    true,
		"ls plain\n":                    false,
		"shopt -s extglob\nls @(a|b)\n": true,
	} {
		all := calls(t, src)
		last := all[len(all)-1]
		if got := isGlob(last.Args[len(last.Args)-1]); got != want {
			t.Errorf("isGlob(%q) = %t, want %t", src, got, want)
		}
	}
}

func TestEffectiveCommandWord(t *testing.T) {
	t.Parallel()

	p := &pass{}
	for src, want := range map[string]string{
		"command -p ls -l\n":      "ls",
		"exec -a name tool --x\n": "tool",
		"time -p make\n":          "make",
		"builtin echo hi\n":       "echo",
		"sudo rm -rf x\n":         "sudo",
		"exec\n":                  "",
	} {
		w := p.effectiveCommandWord(firstCall(t, src))
		got := ""
		if w != nil {
			got, _ = parser.Literal(w)
		}
		if got != want {
			t.Errorf("effectiveCommandWord(%q) = %q, want %q", src, got, want)
		}
	}
}

func TestEnclosingPipeline(t *testing.T) {
	t.Parallel()

	tree := parser.Parse("cat log | grep err | wc -l\necho alone\n", dialect.Bash)
	p := newPass(tree, nil, fileWide{extended: true})
	got := map[string][2]int{}
	syntax.Walk(tree.File, func(n syntax.Node) bool {
		if n == nil {
			p.stack = p.stack[:len(p.stack)-1]
			return true
		}
		if call, ok := n.(*syntax.CallExpr); ok {
			stages, index := p.enclosingPipeline()
			name, _ := parser.Literal(call.Args[0])
			got[name] = [2]int{len(stages), index}
		}
		p.stack = append(p.stack, n)
		return true
	})
	want := map[string][2]int{"cat": {3, 0}, "grep": {3, 1}, "wc": {3, 2}, "echo": {1, 0}}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s: (stages, index) = %v, want %v", name, got[name], w)
		}
	}
}
