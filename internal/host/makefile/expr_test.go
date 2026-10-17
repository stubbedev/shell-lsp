package makefile

import (
	"fmt"
	"strings"
	"testing"
)

func render(text string, e Expr) string {
	var parts []string
	for _, n := range e.Nodes {
		switch n.Kind {
		case NodeText:
			parts = append(parts, fmt.Sprintf("%q", text[n.Span.Start:n.Span.End]))
		case NodeReference:
			parts = append(parts, "ref("+render(text, n.Name)+")")
		case NodeSubstitution:
			parts = append(parts, "subst("+render(text, n.Name)+";"+render(text, n.Args[0])+";"+render(text, n.Args[1])+")")
		case NodeCall:
			args := make([]string, len(n.Args))
			for i, a := range n.Args {
				args[i] = render(text, a)
			}
			parts = append(parts, "call("+render(text, n.Name)+";"+strings.Join(args, ";")+")")
		}
	}
	return strings.Join(parts, " ")
}

func TestExpressionTrees(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ text, want string }{
		{"plain text", `"plain text"`},
		{"$(CC) -o $@", `ref("CC") " -o " ref("@")`},
		{"${CFLAGS}", `ref("CFLAGS")`},
		{"$$HOME", `"$$HOME"`},
		{"$(x(y)z)", `ref("x(y") "z)"`},
		{"${a{b}c}", `ref("a{b") "c}"`},
		{"$(a$(b)c)", `ref("a" ref("b") "c")`},
		{"$(a$(b) x", `ref("a$(b) x")`},
		{"$(x:a=(b))", `subst("x";"a";"(b") ")"`},
		{"$(info a(b)c)", `call("info";"a(b)c")`},
		{"$(SRCS:.c=.o)", `subst("SRCS";".c";".o")`},
		{"$(patsubst %.c,%.o,$(SRCS))", `call("patsubst";"%.c";"%.o";ref("SRCS"))`},
		{"$(if $(X),a,b,c)", `call("if";ref("X");"a";"b";"c")`},
		{"$($(kind)_flags)", `ref(ref("kind") "_flags")`},
		{"$(wild *.c)", `ref("wild *.c")`},
		{"$(call f,(a,b),c)", `call("call";"f";"(a,b)";"c")`},
		{"$(subst a,b,$(x:%=%.o))", `call("subst";"a";"b";subst("x";"%";"%.o"))`},
		{"a$(info )b", `"a" call("info";) "b"`},
	} {
		var errs []Error
		e := exprParser{text: tt.text, errors: &errs}.parse(0, len(tt.text))
		if got := render(tt.text, e); got != tt.want || len(errs) > 0 {
			t.Errorf("parse(%q) = %s with errors %v, want %s", tt.text, got, errs, tt.want)
		}
	}
}

func TestUnterminatedReference(t *testing.T) {
	t.Parallel()

	text := "echo $(CFLAGS"
	var errs []Error
	e := exprParser{text: text, errors: &errs}.parse(0, len(text))
	if len(errs) != 1 || errs[0].Kind != ErrUnterminated || errs[0].Span.Start != 5 {
		t.Errorf("errors = %+v, want one unterminated reference at 5", errs)
	}
	if got := render(text, e); got != `"echo " ref("CFLAGS")` {
		t.Errorf("tree = %s", got)
	}
}
