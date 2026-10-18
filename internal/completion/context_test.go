package completion

import (
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func TestContextsComeFromTheSyntaxTree(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		d       dialect.Dialect
		src     string
		want    []string
		missing []string
	}{
		{name: "flags of a command whose quoted word spans lines", d: dialect.Bash, src: "grep \"a\nb\" --ext‸", want: []string{"--extended-regexp"}},
		{name: "command in a substitution inside arithmetic", d: dialect.Bash, src: "x=1\ny=$(( $(gr‸", want: []string{"grep"}, missing: []string{"x"}},
		{name: "variable after a closed substitution in arithmetic", d: dialect.Bash, src: "count=1\nx=$(( $(true) + cou‸", want: []string{"count"}},
		{name: "variables in an arithmetic subscript", d: dialect.Bash, src: "count=1\necho ${arr[i+cou‸", want: []string{"count"}},
		{name: "variables in a slice offset", d: dialect.Bash, src: "count=1\necho ${name:cou‸", want: []string{"count"}},
		{name: "variables in a slice length", d: dialect.Bash, src: "count=1\necho ${name:1:cou‸", want: []string{"count"}},
		{name: "variables in let", d: dialect.Bash, src: "count=1\nlet x=cou‸", want: []string{"count"}},
		{name: "no variables where an arithmetic operator goes", d: dialect.Bash, src: "count=1\n(( count ‸", missing: []string{"count"}},
		{name: "operator of a nested expansion", d: dialect.Bash, src: "echo \"${x:-${name:‸", want: []string{":-", ":="}},
		{name: "variable inside a nested expansion in quotes", d: dialect.Bash, src: "name=1\necho \"${x:-\"${na‸", want: []string{"name"}},
		{name: "anchored replacement", d: dialect.Bash, src: "echo ${name/#‸", want: []string{"/#"}, missing: []string{"/%", "//"}},
		{name: "replacement of every match", d: dialect.Bash, src: "echo ${name//‸", want: []string{"//"}, missing: []string{"/#"}},
		{name: "no operators inside the operand", d: dialect.Bash, src: "echo ${name:-de‸", missing: []string{":-", ":="}},
		{name: "no operators in single quotes", d: dialect.Bash, src: "echo '${name:‸", missing: []string{":-"}},
		{name: "no variables in a comment inside a substitution", d: dialect.Bash, src: "name=1\nx=$(true # $na‸\n)", missing: []string{"name"}},
		{name: "quoted array key", d: dialect.Bash, src: "declare -A conf=([host]=x)\necho \"${conf[\"ho‸", want: []string{"host"}},
		{name: "zsh short subscript", d: dialect.Zsh, src: "typeset -A h=(alpha 1)\nprint $h[al‸", want: []string{"alpha"}},
		{name: "glob qualifiers after a quoted part", d: dialect.Zsh, src: "print -l \"x\"*(‸", want: []string{"/", "N"}},
		{name: "no glob qualifiers inside a quoted argument", d: dialect.Zsh, src: "print -l *(e:'‸", missing: []string{"/", "N"}},
		{name: "no glob qualifiers in double quotes", d: dialect.Zsh, src: "print \"*(‸", missing: []string{"/", "N"}},
		{name: "no globbing flags in double quotes", d: dialect.Zsh, src: "print \"(#‸", missing: []string{"i", "b"}},
		{name: "zsh flags of a nested expansion", d: dialect.Zsh, src: "print ${(j:,:)${(U‸", want: []string{"j"}, missing: []string{"U"}},
		{name: "unary test after a closed test", d: dialect.Bash, src: "if [[ $a == b ]] && [[ -‸", want: []string{"-f", "-z"}},
		{name: "binary test in parentheses", d: dialect.Bash, src: "[[ ( $a ‸", want: []string{"==", "-eq"}, missing: []string{"-f"}},
		{name: "binary test being typed", d: dialect.Bash, src: "[[ $a -e‸", want: []string{"-eq"}, missing: []string{"-f", "=="}},
		{name: "binary test after a connective", d: dialect.Bash, src: "[[ $a == b && $c ‸", want: []string{"=="}},
		{name: "test closed later on the line", d: dialect.Bash, src: "[[ -‸ ]]", want: []string{"-f"}},
		{name: "variable in a quoted test operand", d: dialect.Bash, src: "name=1\nif [[ -n \"$na‸", want: []string{"name"}},
		{name: "variable in a quoted operand inside a substitution in a test", d: dialect.Bash, src: "name=1\n[[ $(echo \"${na‸", want: []string{"name"}},
		{name: "command in a substitution in a test", d: dialect.Bash, src: "[[ -n $(gr‸", want: []string{"grep"}},
		{name: "declaration names", d: dialect.Bash, src: "count=1\nexport cou‸", want: []string{"count"}},
		{name: "declaration flags after a name", d: dialect.Bash, src: "declare x -‸", want: []string{"-A", "-a"}},
		{name: "redirection target", d: dialect.Bash, src: "git > ‸", want: []string{"lib/", "main.sh"}, missing: []string{"checkout"}},
		{name: "no paths for a descriptor", d: dialect.Bash, src: "echo hi >&‸", missing: []string{"lib/"}},
		{name: "loop words", d: dialect.Bash, src: "for f in ma‸", want: []string{"main.sh"}},
		{name: "no completion of a loop variable name", d: dialect.Bash, src: "for ma‸", missing: []string{"main.sh"}},
		{name: "command after a compound command", d: dialect.Bash, src: "if true; then :; fi\ngr‸", want: []string{"grep"}},
		{name: "command in a branch", d: dialect.Bash, src: "if true; then\n\t:\nelse gr‸", want: []string{"grep"}},
		{name: "argument after a pipeline", d: dialect.Bash, src: "ls | grep --ext‸", want: []string{"--extended-regexp"}},
		{name: "argument in a function body", d: dialect.Bash, src: "f() {\n\tgrep --ext‸\n}\n", want: []string{"--extended-regexp"}},
		{name: "after an unrelated syntax error", d: dialect.Bash, src: "echo ${a[1 2]}\ngr‸", want: []string{"grep"}},
		{name: "expansion operator in a heredoc", d: dialect.Bash, src: "cat <<EOF\n${name:‸\nEOF\n", want: []string{":-"}},
		{name: "command in a heredoc substitution", d: dialect.Bash, src: "cat <<EOF\n$(gr‸\nEOF\n", want: []string{"grep"}},
		{name: "no printf directives inside a strftime format", d: dialect.Bash, src: "printf '%(%‸", missing: []string{"%s"}},
		{name: "printf directives after an escaped percent", d: dialect.Bash, src: "printf '%%%‸", want: []string{"%s"}},
		{name: "printf directives in a format with an expansion", d: dialect.Bash, src: "printf \"$x %-5‸", want: []string{"%s"}},
	}
	for _, tc := range tests {
		got := labels(complete(t, tc.d, tc.src))
		for _, want := range tc.want {
			if !slices.Contains(got, want) {
				t.Errorf("%s: %v lacks %q", tc.name, got, want)
			}
		}
		for _, missing := range tc.missing {
			if slices.Contains(got, missing) {
				t.Errorf("%s: %v offers %q", tc.name, got, missing)
			}
		}
	}
}

func TestCommandAtReadsTheCommandAroundTheCursor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src     string
		command string
		args    int
		ok      bool
	}{
		{src: "grep -i pat‸", command: "grep", args: 1, ok: true},
		{src: "FOO=1 sudo -u root ‸", command: "sudo", args: 2, ok: true},
		{src: "x=$(printf '%s' ‸)", command: "printf", args: 1, ok: true},
		{src: "[[ -f a && ‸", command: "[[", args: 3, ok: true},
		{src: "declare -A ‸", command: "declare", args: 1, ok: true},
		{src: "gr‸", ok: false},
		{src: "echo ${na‸", ok: false},
	}
	for _, tc := range tests {
		offset := strings.Index(tc.src, cursor)
		tree := parser.Parse(strings.Replace(tc.src, cursor, "", 1), dialect.Bash)
		command, args, ok := CommandAt(tree, offset)
		if command != tc.command || args != tc.args || ok != tc.ok {
			t.Errorf("CommandAt(%q) = %q, %d, %t, want %q, %d, %t", tc.src, command, args, ok, tc.command, tc.args, tc.ok)
		}
	}
}

func TestReplacedSpansOfSyntaxTreeContexts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, src, label, replaced string
		d                          dialect.Dialect
	}{
		{name: "variable", d: dialect.Bash, src: "count=1\necho \"${cou‸", label: "count", replaced: "cou"},
		{name: "length of a variable", d: dialect.Bash, src: "count=1\necho ${#cou‸", label: "count", replaced: "cou"},
		{name: "arithmetic", d: dialect.Bash, src: "count=1\n(( x = cou‸", label: "count", replaced: "cou"},
		{name: "operator", d: dialect.Bash, src: "echo ${x%%‸", label: "%%", replaced: "%%"},
		{name: "anchored replacement", d: dialect.Bash, src: "echo ${x/%‸", label: "/%", replaced: "/%"},
		{name: "array key", d: dialect.Bash, src: "declare -A m=([alpha]=1)\necho ${m[al‸", label: "alpha", replaced: "al"},
		{name: "quoted command word", d: dialect.Bash, src: "cat \"my‸", label: "my file.sh", replaced: "\"my"},
		{name: "test operator", d: dialect.Bash, src: "[[ $a -e‸", label: "-eq", replaced: "-e"},
		{name: "case pattern", d: dialect.Bash, src: "[ \"$1\" = start ]\ncase $1 in st‸", label: "start", replaced: "st"},
	}
	for _, tc := range tests {
		offset := strings.Index(tc.src, cursor)
		text := strings.Replace(tc.src, cursor, "", 1)
		it, ok := find(complete(t, tc.d, tc.src), tc.label)
		if !ok {
			t.Errorf("%s: %q not offered", tc.name, tc.label)
			continue
		}
		if it.Replace.End != offset || text[it.Replace.Start:it.Replace.End] != tc.replaced {
			t.Errorf("%s: replaces %q, want %q", tc.name, text[it.Replace.Start:it.Replace.End], tc.replaced)
		}
	}
}
