package parser

import (
	"slices"
	"strings"
	"testing"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

func callArgs(t *testing.T, src string) []*syntax.Word {
	t.Helper()

	call, ok := Parse(src, dialect.Bash).File.Stmts[0].Cmd.(*syntax.CallExpr)
	if !ok {
		t.Fatalf("%q does not parse to a call", src)
	}
	return call.Args
}

func describeInvocations(w Wrappers, args []*syntax.Word) []string {
	var out []string
	for run := range w.Invocations(args) {
		words := make([]string, len(run.Args))
		for i, word := range run.Args {
			words[i], _ = Text(word, "$")
		}
		described := strings.Join(words, " ")
		if run.Placeholder != "" {
			described += " [" + run.Placeholder + "]"
		}
		if !run.InheritsStdin {
			described += " [no stdin]"
		}
		out = append(out, described)
	}
	return out
}

func TestInvocations(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		src  string
		want []string
	}{
		{"awk 1 f\n", []string{"awk 1 f"}},
		{"command -p awk 1\n", []string{"awk 1"}},
		{"sudo -u root -E awk 1\n", []string{"awk 1"}},
		{"env -i LC_ALL=C awk 1\n", []string{"awk 1"}},
		{"nice -n 10 nohup timeout 5 awk 1\n", []string{"awk 1"}},
		{"timeout\n", nil},
		{"sudo \"$cmd\" x\n", []string{"$ x"}},
		{"xargs awk 1\n", []string{"awk 1 [no stdin]"}},
		{"xargs -0 -n1 -P 4 sed p\n", []string{"sed p [no stdin]"}},
		{"xargs -I {} sed s/{}/x/ f\n", []string{"sed s/{}/x/ f [{}] [no stdin]"}},
		{"xargs -I% sed s/%/x/ f\n", []string{"sed s/%/x/ f [%] [no stdin]"}},
		{"xargs -J @ cp @ dir\n", []string{"cp @ dir [@] [no stdin]"}},
		{"xargs -i sed p\n", []string{"sed p [{}] [no stdin]"}},
		{"xargs -iX sed p\n", []string{"sed p [X] [no stdin]"}},
		{"xargs --replace sed p\n", []string{"sed p [{}] [no stdin]"}},
		{"xargs --replace=@ sed p\n", []string{"sed p [@] [no stdin]"}},
		{"xargs -I \"$r\" sed p\n", nil},
		{"sudo env A=1 nice timeout 3 xargs awk 1\n", []string{"awk 1 [no stdin]"}},
		{"find . -name '*.txt' -exec awk 1 {} \\;\n", []string{"awk 1 {} [{}]"}},
		{"find . -exec sed -i p {} + -print\n", []string{"sed -i p {} [{}]"}},
		{"find . -execdir awk 1 {} ';' -ok sed p {} \\;\n", []string{"awk 1 {} [{}]", "sed p {} [{}]"}},
		{"find . -exec awk 1 + \\;\n", []string{"awk 1 + [{}]"}},
		{"find . -okdir sudo awk 1 {} +\n", []string{"awk 1 {} [{}]"}},
		{"sudo find / -exec awk 1 {} +\n", []string{"awk 1 {} [{}]"}},
		{"xargs -I {} find {} -exec awk 1 \\;\n", []string{"awk 1 [{}] [no stdin]"}},
		{"xargs -I % find % -exec awk 1 \\;\n", nil},
		{"find . -exec awk 1 {}\n", nil},
		{"find . -exec \\;\n", nil},
		{"find . -name x\n", nil},
	} {
		if got := describeInvocations(Wrappers{Programs: true}, callArgs(t, tt.src)); !slices.Equal(got, tt.want) {
			t.Errorf("Invocations(%q) = %q, want %q", tt.src, got, tt.want)
		}
	}
}

func TestInvocationsLookThroughProgramRunnersOnlyWhenAsked(t *testing.T) {
	t.Parallel()

	for src, want := range map[string]string{
		"sudo awk 1\n":                   "sudo awk 1",
		"find . -exec awk 1 {} \\;\n":    "find . -exec awk 1 {} ;",
		"command xargs -I {} awk 1\n":    "xargs -I {} awk 1",
		"run sudo awk 1\n":               "run sudo awk 1",
		"exec -a name nice -n 5 awk 1\n": "nice -n 5 awk 1",
	} {
		if got := describeInvocations(Wrappers{}, callArgs(t, src)); !slices.Equal(got, []string{want}) {
			t.Errorf("Invocations(%q) without program runners = %q, want %q", src, got, want)
		}
	}
}

func TestInvocationsStopEarly(t *testing.T) {
	t.Parallel()

	var got []string
	for run := range (Wrappers{Programs: true}).Invocations(callArgs(t, "find . -exec awk 1 \\; -exec sed p \\;\n")) {
		name, _ := Literal(run.Args[0])
		got = append(got, name)
		break
	}
	if !slices.Equal(got, []string{"awk"}) {
		t.Errorf("invocations before stopping = %q, want only awk", got)
	}
}

func TestRuns(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		wrappers Wrappers
		name     string
		want     bool
	}{
		{Wrappers{}, "command", true},
		{Wrappers{}, "sudo", false},
		{Wrappers{}, "find", false},
		{Wrappers{Programs: true}, "sudo", true},
		{Wrappers{Programs: true}, "find", true},
		{Wrappers{Programs: true}, "awk", false},
	} {
		if got := tt.wrappers.Runs(tt.name); got != tt.want {
			t.Errorf("%+v.Runs(%q) = %t, want %t", tt.wrappers, tt.name, got, tt.want)
		}
	}
}
