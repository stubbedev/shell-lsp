package projection

import "testing"

func TestPlaceholderLeavesSingleQuotes(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, before string
		single       bool
	}{
		{"plain word", "echo ", false},
		{"single quotes", "echo 'a ", true},
		{"closed single quotes", "echo 'a' ", false},
		{"double quotes", `echo "it's `, false},
		{"escaped quote", `echo \'`, false},
		{"single quote holding a backslash", `echo 'a\`, true},
		{"comment", "# don't\necho ", false},
		{"apostrophe in a heredoc", "cat <<EOF\nit's\nEOF\necho ", false},
		{"tab-stripped heredoc", "cat <<-'END'\n\tdon't\n\tEND\necho '", true},
		{"inside a heredoc", "cat <<EOF\n'", false},
		{"multi-line single quotes", "php -r '\n$a = 1;\n", true},
	} {
		host := tt.before + "X"
		b := NewBuilder(host)
		b.Region(func(_ string, _, _ int, text string) (string, bool) { return text, true })
		b.Verbatim(0, len(tt.before))
		b.Placeholder(len(tt.before), len(host))
		want := tt.before + Dynamic
		if tt.single {
			want = tt.before + "'" + Dynamic + "'"
		}
		if got := b.Map().Text(); got != want {
			t.Errorf("%s: %q, want %q", tt.name, got, want)
		}
	}
}

func TestScriptsStartWithFreshQuoting(t *testing.T) {
	t.Parallel()

	host := "echo 'aX"
	b := NewBuilder(host)
	b.Region(func(_ string, _, _ int, text string) (string, bool) { return text, true })
	b.Verbatim(0, 7)
	b.Script()
	b.Placeholder(7, 8)
	if got := b.Map().Text(); got != "echo 'a"+Dynamic {
		t.Errorf("a new script inherited the quoting of the last: %q", got)
	}
}
