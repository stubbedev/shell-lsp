package actions

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

func TestDoubleBrackets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src, want string
	}{
		{src: "«[» -f \"$file\" ]\n", want: "[[ -f \"$file\" ]]\n"},
		{src: "if test -n \"$«x»\"; then :; fi\n", want: "if [[ -n \"$x\" ]]; then :; fi\n"},
		{src: "[ «-n» \"$a\" -a \\( \"$b\" \\< c -o ! -f x \\) ]\n", want: "[[ -n \"$a\" && ( \"$b\" < c || ! -f x ) ]]\n"},
		{src: "[ \"$a\" '=' «b» ]\n", want: "[[ \"$a\" = b ]]\n"},
		{src: "[ $# «-eq» 0 ]\n", want: "[[ $# -eq 0 ]]\n"},
		{src: "[ \"$a\" «=» \"$b\" ]\n", want: "[[ \"$a\" = \"$b\" ]]\n"},
		{src: "[ \"$(«w»hoami)\" = root ]\n", want: "[[ \"$(whoami)\" = root ]]\n"},
		{src: "«test» \"$a\" == 'b*'\n", want: "[[ \"$a\" == 'b*' ]]\n"},
		{src: "! «[» -f x ]\n", want: "! [[ -f x ]]\n"},
		{src: "«[» -f x ] 2>/dev/null && echo yes\n", want: "[[ -f x ]] 2>/dev/null && echo yes\n"},
		{src: "[ «\"$a\"» ]\n", want: "[[ \"$a\" ]]\n"},
		{src: "[ $((«n» + 1)) -gt 2 ]\n", want: "[[ $((n + 1)) -gt 2 ]]\n"},
	}
	for _, tc := range tests {
		got, title, ok := refactor(t, DoubleBrackets, tc.src)
		if !ok || got != tc.want || title != "Convert to [[ ]]" {
			t.Errorf("convert %q = %q (%q, %t), want %q", tc.src, got, title, ok, tc.want)
		}
	}
	for _, src := range []string{
		"[ «\"$a\"» = $b ]\n",
		"[ «\"$a\"» = *.txt ]\n",
		"[ «-f» *.txt ]\n",
		"[ «\"$a\"» = {a,b} ]\n",
		"[ «-n» $var ]\n",
		"[ «\"$a\"» = `date` ]\n",
		"[ «a» > b ]\n",
		"FOO=1 «[» -f x ]\n",
		"«[» -n ]\n",
		"«[» ]\n",
		"«[» -f x\n",
		"[ «\"$a\"» =~ b ]\n",
		"/usr/bin/test «-f» x\n",
		"[[ «-f» x ]]\n",
		"[ «\"$a\"» -foo b ]\n",
		"«echo» [ x ]\n",
	} {
		if got, _, ok := refactor(t, DoubleBrackets, src); ok {
			t.Errorf("convert %q offered: %q", src, got)
		}
	}
	for _, d := range []dialect.Dialect{dialect.Posix, dialect.Dash} {
		if got, _, ok := refactorIn(t, d, DoubleBrackets, "«[» -f x ]\n"); ok {
			t.Errorf("convert offered in %s, which has no [[: %q", d, got)
		}
	}
	for _, d := range []dialect.Dialect{dialect.Zsh, dialect.Ksh} {
		if got, _, ok := refactorIn(t, d, DoubleBrackets, "«[» -f x ]\n"); !ok || got != "[[ -f x ]]\n" {
			t.Errorf("convert in %s = %q (%t)", d, got, ok)
		}
	}
}
