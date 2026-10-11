package lint

import (
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

func calls(t *testing.T, src string) []*syntax.CallExpr {
	t.Helper()
	var out []*syntax.CallExpr
	parser.Walk(parser.Parse(src, dialect.Bash).File, func(n syntax.Node) bool {
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

func TestEnclosingPipeline(t *testing.T) {
	t.Parallel()

	tree := parser.Parse("cat log | grep err | wc -l\necho alone\n", dialect.Bash)
	p := newPass(tree, nil, Options{})
	got := map[string][2]int{}
	parser.Walk(tree.File, func(n syntax.Node) bool {
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

func TestCommandDispatch(t *testing.T) {
	t.Parallel()

	var seen []string
	record := func(label string) func(*pass, command) {
		return func(_ *pass, c command) { seen = append(seen, label+":"+c.name) }
	}
	d := newCommandDispatch([]commandRule{
		{match: matchExact, names: []string{"echo", "local"}, check: record("exact")},
		{match: matchBase, names: []string{"echo"}, check: record("base")},
	})
	for src, want := range map[string][]string{
		"echo hi\n":          {"base:echo", "exact:echo"},
		"/bin/echo hi\n":     {"base:/bin/echo"},
		"builtin echo hi\n":  {"exact:echo"},
		"$cmd hi\n":          nil,
		"f() { local x; }\n": {"exact:local"},
	} {
		seen = nil
		parser.Walk(parser.Parse(src, dialect.Bash).File, func(n syntax.Node) bool {
			d.visit(nil, n)
			return true
		})
		if !slices.Equal(seen, want) {
			t.Errorf("dispatch(%q) = %v, want %v", src, seen, want)
		}
	}
}

func TestOptionGrammar(t *testing.T) {
	t.Parallel()

	render := func(o parsedOptions, ok bool) string {
		if !ok {
			return "fail"
		}
		var parts []string
		for _, opt := range o.options {
			entry := opt.name
			if opt.value != nil {
				value, _ := parser.Literal(opt.value)
				entry += "=" + value
			}
			parts = append(parts, entry)
		}
		for _, w := range o.operands {
			lit, _ := parser.Literal(w)
			parts = append(parts, "["+lit+"]")
		}
		return strings.Join(parts, " ")
	}
	for _, tc := range []struct {
		grammar optionGrammar
		args    string
		want    string
	}{
		{optionGrammar{short: "f:"}, "-f x", "f=x"},
		{optionGrammar{short: "f:"}, "-fx", "f=-fx"},
		{optionGrammar{short: "fx"}, "-f -x", "f x"},
		{optionGrammar{short: "f:"}, "-f -x", "f=-x"},
		{optionGrammar{short: "fx:"}, "-fx", "fail"},
		{optionGrammar{long: map[string]bool{"foo": true}}, "--foo=bar baz", "foo=--foo=bar [baz]"},
		{optionGrammar{long: map[string]bool{"foo": true}}, "--foo bar baz", "foo=bar [baz]"},
		{optionGrammar{anyLong: true}, "--foo baz", "foo [baz]"},
		{optionGrammar{}, "--foo baz", "fail"},
		{optionGrammar{short: "ab"}, "-a x -b", "a b [x]"},
		{optionGrammar{short: "ab", stopAtOperand: true}, "-a x -b", "a [x] [-b]"},
		{optionGrammar{short: "ab"}, "-a -- -b", "a [-b]"},
		{optionGrammar{short: "a:b"}, "-a -- -b", "a=-- b"},
	} {
		got := render(tc.grammar.parse(firstCall(t, "cmd "+tc.args+"\n").Args[1:]))
		if got != tc.want {
			t.Errorf("%+v parse(%q) = %q, want %q", tc.grammar, tc.args, got, tc.want)
		}
	}
}
