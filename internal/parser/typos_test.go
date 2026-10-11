package parser

import (
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

func TestTypoHints(t *testing.T) {
	t.Parallel()

	for src, want := range map[string]string{
		"make\n&& make install\n":                           "a line cannot start with `&&`",
		"if! grep -q x f; then echo y; fi\n":                "`if!` is one word",
		"if true; then:; fi\n":                              "`then:` is one word",
		"For f in *.txt; do echo; done\n":                   "write `for`",
		"if [ a ]; then echo\nelsif [ b ]; then echo\nfi\n": "write `elif`",
		"if [[ -f a ]] or [[ -f b ]]; then :; fi\n":         "join test commands with `||`, not `or`",
		"{ echo a; } extra\n":                               "`extra` cannot follow `}` directly",
		"for $f in *; do echo; done\n":                      "write `for f` without `$`",
		"for ( (i=0; i<3; i++)); do :; done\n":              "without spaces",
		"for (i=0; i<3; i++)); do :; done\n":                "needs double parentheses",
		"total=((3 + 4))\n":                                 "use `$((...))`",
		"if (( n -gt 3 )); then :; fi\n":                    "compare with `>`, not `-gt`",
		"while read -r l; do :; done <(ls)\n":               "`done < <(...)`",
		"eval items=(a b c)\n":                              "quote the arguments of eval",
	} {
		tree := Parse(src, dialect.Bash)
		if !strings.Contains(strings.Join(messages(tree), "\n"), want) {
			t.Errorf("%q: messages %q do not contain %q", src, messages(tree), want)
		}
	}
}

func TestTypoHintsOnALongLineLookNearTheError(t *testing.T) {
	t.Parallel()

	text := "true " + strings.Repeat("&&", 2000) + " { echo a; } extra\n"
	hint := typoHint(text, strings.Index(text, "extra"), dialect.Bash)
	if !strings.Contains(hint, "`extra` cannot follow `}` directly") || strings.Contains(hint, "cannot start") {
		t.Errorf("hint %q, want only the one for extra", hint)
	}

	far := "for $f in *; do :; done; " + strings.Repeat("echo a; ", 100_000) + ")\n"
	if hint := typoHint(far, strings.LastIndexByte(far, ')'), dialect.Bash); hint != "" {
		t.Errorf("hint %q describes text 800 KB before the error", hint)
	}
}

func TestNoTypoHintForTheErrorItself(t *testing.T) {
	t.Parallel()

	tree := Parse("shopt -s extglob\nls +(name\\)  )\n", dialect.Bash)
	for _, m := range messages(tree) {
		if strings.Contains(m, "cannot follow") {
			t.Errorf("message %q has a hint for a closing parenthesis that is the error itself", m)
		}
	}
}
