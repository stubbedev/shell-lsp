package prose

import "testing"

func TestListJoinsTheLastItemWithTheConjunction(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		items []string
		want  string
	}{
		{items: nil, want: ""},
		{items: []string{"sh"}, want: "sh"},
		{items: []string{"sh", "bash"}, want: "sh or bash"},
		{items: []string{"sh", "bash", "zsh"}, want: "sh, bash or zsh"},
	} {
		if got := List(tc.items, "or"); got != tc.want {
			t.Errorf("List(%q) = %q, want %q", tc.items, got, tc.want)
		}
	}
}

func TestCodeFencesTextThatHoldsBackticks(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ in, want string }{
		{in: "*.c", want: "`*.c`"},
		{in: "a`b", want: "``a`b``"},
		{in: "`x`", want: "`` `x` ``"},
	} {
		if got := Code(tc.in); got != tc.want {
			t.Errorf("Code(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCodeBlockFencesTextThatHoldsFences(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ in, want string }{
		{in: "ls -l", want: "```\nls -l\n```"},
		{in: "```\nx\n```", want: "````\n```\nx\n```\n````"},
	} {
		if got := CodeBlock(tc.in); got != tc.want {
			t.Errorf("CodeBlock(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEscapeKeepsMarkdownSyntaxLiteral(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ in, want string }{
		{in: "plain words, (and) dots.", want: "plain words, (and) dots."},
		{in: "named <new-branch>", want: `named \<new-branch\>`},
		{in: "*glob* snake_case `x` [a|b] a&b ~x \\", want: `\*glob\* snake\_case \` + "`" + `x\` + "`" + ` \[a\|b\] a\&b \~x \\`},
		{in: "# title", want: `\# title`},
		{in: "- item\n  + item\n= rule", want: "\\- item\n  \\+ item\n\\= rule"},
		{in: "1. first\n2) second\n10 items", want: "1\\. first\n2\\) second\n10 items"},
		{in: "a - b # c", want: "a - b # c"},
	} {
		if got := Escape(tc.in); got != tc.want {
			t.Errorf("Escape(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
