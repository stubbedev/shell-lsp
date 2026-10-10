package parser

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

func TestSyntaxErrorsExplainTheLikelyMistake(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src     string
		message string
		at      string
	}{
		{src: "cat <<END\nusage\n  END\n", message: "here-document `END` is never closed: the end token on line 3 is indented; remove the indentation, or use <<- and indent with tabs", at: "<<END"},
		{src: "cat <<-END\nusage\n    END\n", message: "here-document `END` is never closed: line 3 indents the end token with spaces, but <<- strips only tabs", at: "<<-END"},
		{src: "cat <<EOF\nbody\nEOF.\n", message: "here-document `EOF` is never closed: line 3 (\"EOF.\") contains the end token, but it must stand alone on its line", at: "<<EOF"},
		{src: "cat <<EOF\nbody\neof\n", message: "here-document `EOF` is never closed: line 3 (\"eof\") has the end token in different letter case", at: "<<EOF"},
		{src: "cat <<EOF\nbody\n", message: "here-document `EOF` is never closed; end it with a line containing only `EOF`", at: "<<EOF"},
		{src: "sleep 1 &; echo done\n", message: "`&` already ends the command and runs it in the background; remove the `;` after it", at: ";"},
		{src: "if true; then; echo x; fi\n", message: "remove the `;` after `then`; the keyword is already followed by the commands it introduces", at: "then;"},
		{src: "for f in a; do; echo x; done\n", message: "remove the `;` after `do`; the keyword is already followed by the commands it introduces", at: "do;"},
		{src: "{date; } > out\n", message: "put a space after `{` in `{date;`; glued to a word, the brace does not open a block", at: "}"},
		{src: "add(a, b) { :; }\n", message: "a function definition takes no parameter list; write `add()` and read the arguments as $1, $2, ... in the body", at: "add(a,"},
		{src: "if [ ( a = b ) ]; then :; fi\n", message: "inside `[ ]`, parentheses must be escaped as `\\(` and `\\)`", at: "["},
		{src: "sort << (ls)\n", message: "`<<` starts a here-document; to read from a process substitution write `< <(...)` with a space between the two `<`", at: "(ls)"},
		{src: "$fruits=(apple pear)\n", message: "remove the `$` from `$fruits`: an assignment names the variable without expanding it", at: "=(apple"},
		{src: "if[ -d /tmp ]; then :; fi\n", message: "put a space between `if` and `[`; glued together they form one word instead of a keyword and a test", at: "["},
		{src: "if [[ $1 = x ]; then :; fi\n", message: "a test opened with `[[` must be closed with `]]`, not `]`", at: "];"},
		{src: "if [[ $1 = x]]; then :; fi\n", message: "put a space before `]]`; glued to `x` it does not close the test", at: ";"},
		{src: "if [[ -z ]]; then :; fi\n", message: "`-z` is missing its operand before `]]`", at: ";"},
		{src: "if [[ [ -d a ] ]]; then :; fi\n", message: "inside `[[ ]]`, group conditions with `( )`, not with nested `[ ]`", at: "-d"},
		{src: "if [[ \\( $1 = x \\) ]]; then :; fi\n", message: "inside `[[ ]]`, group conditions with plain `( )`; do not escape or quote the parentheses", at: "$1"},
		{src: "if [[ echo \"$x\" ]]; then :; fi\n", message: "expected a test operator after `echo`; to check whether a command succeeds, run it without `[[ ]]`", at: "\"$x\""},
		{src: "for h in a b do\n  echo \"$h\"\ndone\n", message: "`do` needs a `;` or line break before it; here it is read as one more word of the list", at: "do"},
		{src: "if true; then\n  :\nelse\nfi\n", message: "expected a command after `else`", at: "else"},
	}
	for _, tc := range tests {
		tree := Parse(tc.src, dialect.Bash)
		found := false
		for _, e := range tree.Errors {
			if e.Message == tc.message && tc.src[e.Span.Start:e.Span.End] == tc.at {
				found = true
			}
		}
		if !found {
			t.Errorf("Parse(%q) errors = %+v, want %q at %q", tc.src, tree.Errors, tc.message, tc.at)
		}
	}
}
