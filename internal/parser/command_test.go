package parser

import (
	"testing"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

func TestCommandStart(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		src  string
		bats bool
		want string
	}{
		{"command -p ls -l\n", false, "ls"},
		{"exec -a name tool --x\n", false, "tool"},
		{"command time -p make\n", false, "make"},
		{"builtin echo hi\n", false, "echo"},
		{"busybox awk 1\n", false, "awk"},
		{"sudo rm -rf x\n", false, "sudo"},
		{"run jq .\n", false, "run"},
		{"run jq .\n", true, "jq"},
		{"exec\n", false, ""},
	} {
		call, ok := Parse(tt.src, dialect.Bash).File.Stmts[0].Cmd.(*syntax.CallExpr)
		if !ok {
			t.Fatalf("%q does not parse to a call", tt.src)
		}
		got := ""
		if i := CommandStart(call.Args, tt.bats); i >= 0 {
			got, _ = Literal(call.Args[i])
		}
		if got != tt.want {
			t.Errorf("CommandStart(%q, bats %t) = %q, want %q", tt.src, tt.bats, got, tt.want)
		}
	}
}
