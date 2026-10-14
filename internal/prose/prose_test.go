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
