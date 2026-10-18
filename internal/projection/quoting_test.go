package projection

import (
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/parser"
)

func keep(_ string, _, _ int, text string) (string, bool) { return text, true }

func TestPlaceholderLeavesSingleQuotes(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, before, after, want string
	}{
		{"plain word", "echo ", "", "$$"},
		{"single quotes", "echo 'a ", "'", "'$$'"},
		{"closed single quotes", "echo 'a' ", "", "$$"},
		{"double quotes", `echo "it's `, `"`, "$$"},
		{"escaped quote", `echo \'`, "", "$$"},
		{"single quote holding a backslash", `echo 'a\`, "'", "'$$'"},
		{"comment", "# don't\necho ", "", "$$"},
		{"apostrophe in a heredoc", "cat <<EOF\nit's\nEOF\necho ", "", "$$"},
		{"tab-stripped heredoc", "cat <<-'END'\n\tdon't\n\tEND\necho '", "'", "'$$'"},
		{"inside a heredoc", "cat <<EOF\n'", "\nEOF", "$$"},
		{"multi-line single quotes", "php -r '\n$a = 1;\n", "'", "'$$'"},
		{"ANSI-C quotes", `echo $'a\t`, `\n'`, "'$$$'"},
		{"single quotes in a command substitution in double quotes", `echo "$(echo '`, `')"`, "'$$'"},
		{"single quotes in a command substitution in a heredoc", "cat <<EOF\n$(echo '", "')\nEOF", "'$$'"},
		{"after an arithmetic shift", "echo $((1<<2))\necho '", "'", "'$$'"},
		{"after a heredoc operator in a comment", "# cat <<EOF\necho '", "'", "'$$'"},
		{"after a heredoc operator in double quotes", "echo \"<<EOF\"\necho '", "'", "'$$'"},
		{"after a here-string", "cat <<<EOF\necho '", "'", "'$$'"},
	} {
		host := tt.before + "X" + tt.after
		b := NewBuilder(host)
		b.Region(keep)
		b.Verbatim(0, len(tt.before))
		b.Placeholder(len(tt.before), len(tt.before)+1)
		b.Verbatim(len(tt.before)+1, len(host))
		if got, want := b.Map().Text(), tt.before+tt.want+tt.after; got != want {
			t.Errorf("%s: %q, want %q", tt.name, got, want)
		}
	}
}

func TestScriptsStartWithFreshQuoting(t *testing.T) {
	t.Parallel()

	host := "echo 'aX"
	b := NewBuilder(host)
	b.Region(keep)
	b.Verbatim(0, 7)
	b.Script()
	b.Placeholder(7, 8)
	if got := b.Map().Text(); got != "echo 'a"+Dynamic {
		t.Errorf("a new script inherited the quoting of the last: %q", got)
	}
}

func TestClosedQuotesKeepOffsets(t *testing.T) {
	t.Parallel()

	host := "echo 'X' Y\necho 'Z'\n"
	b := NewBuilder(host)
	b.Region(keep)
	b.Verbatim(0, 6)
	b.Placeholder(6, 7)
	b.Verbatim(7, 9)
	b.Placeholder(9, 10)
	b.Verbatim(10, 11)
	b.Script()
	b.Verbatim(11, 17)
	b.Placeholder(17, 18)
	b.Verbatim(18, len(host))
	m := b.Map()
	text := m.Text()
	if want := "echo ''$$'' $$\necho ''$$''\n"; text != want {
		t.Fatalf("%q, want %q", text, want)
	}
	if want := []parser.Span{{Start: 0, End: 15}, {Start: 15, End: len(text)}}; len(m.Scripts()) != 2 || m.Scripts()[0] != want[0] || m.Scripts()[1] != want[1] {
		t.Errorf("Scripts() = %v, want %v", m.Scripts(), want)
	}
	for _, word := range []string{"' ", "\necho", "'\n"} {
		proj := strings.LastIndex(text, word)
		hostAt := strings.LastIndex(host, word)
		if got := m.HostOffset(ProjOffset(proj)); got != HostOffset(hostAt) {
			t.Errorf("HostOffset(%d) of %q = %d, want %d", proj, word, got, hostAt)
		}
		if got, inside := m.Offset(HostOffset(hostAt)); got != ProjOffset(proj) || !inside {
			t.Errorf("Offset(%d) of %q = %d, %t, want %d, true", hostAt, word, got, inside, proj)
		}
	}
}
