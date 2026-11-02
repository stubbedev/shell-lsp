package nix

import (
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func TestBadDisabledCodes(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src string
		want      []string
	}{
		{"a word", "# shellcheck disable=nonsense\n1\n", []string{"SC1125 nonsense"}},
		{"a word after a good code", "# shellcheck disable=SL8005,nonsense\n1\n", []string{"SC1125 SL8005,nonsense"}},
		{"a good code still silences beside it", "# shellcheck disable=SL8005,nonsense\nlet a = 1; in 2\n", []string{"SC1125 SL8005,nonsense"}},
		{"a same-line directive", "let\n  a = 1; # shellcheck disable=nonsense\nin a\n", []string{"SC1125 nonsense"}},
		{"another key is not checked", "# shellcheck source=nonsense\n1\n", nil},
		{"a good code", "# shellcheck disable=SL8005\n1\n", nil},
		{"a broken file waits", "# shellcheck disable=nonsense\n{ a = ; }\n", []string{"SL8001 ;"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := summaries(tt.src, findingsOf(tt.src)); !slices.Equal(got, tt.want) {
				t.Errorf("%q reports %q, want %q", tt.src, got, tt.want)
			}
		})
	}
}

func TestUnusedDirectivesWhenOptedIn(t *testing.T) {
	t.Parallel()

	const enable = "# shellcheck enable=unused-directives\n"
	for _, tt := range []struct {
		name, src string
		want      []string
		fixed     string
	}{
		{name: "off by default", src: "# shellcheck disable=SL8006\nlet a = 1; in 2\n", want: []string{"SL8005 a"}},
		{name: "unused", src: enable + "# shellcheck disable=SL8006\n1\n", want: []string{"SL2009 SL8006"}, fixed: enable + "1\n"},
		{name: "used", src: enable + "# shellcheck disable=SL8005\nlet a = 1; in 2\n"},
		{name: "one of a list", src: enable + "# shellcheck disable=SL8006,SL8005\nlet a = 1; in 2\n", want: []string{"SL2009 SL8006"}, fixed: enable + "# shellcheck disable=SL8005\nlet a = 1; in 2\n"},
		{name: "the last of a list", src: enable + "# shellcheck disable=SL8005,SL8006\nlet a = 1; in 2\n", want: []string{"SL2009 SL8006"}, fixed: enable + "# shellcheck disable=SL8005\nlet a = 1; in 2\n"},
		{name: "a shell code", src: enable + "# shellcheck disable=SC2086\n1\n", want: []string{"SL2009 SC2086"}, fixed: enable + "1\n"},
		{name: "all silences itself", src: enable + "# shellcheck disable=all\n1\n"},
		{name: "a directive that silences the check", src: enable + "# shellcheck disable=SL2009,SL8006\n1\n"},
		{name: "a directive that reaches nothing", src: enable + "let\n  a = 1;\n  # shellcheck disable=SL8006\nin a\n"},
		{name: "enabling all", src: "# shellcheck enable=all\n# shellcheck disable=SL8006\n1\n", want: []string{"SL2009 SL8006"}, fixed: "# shellcheck enable=all\n1\n"},
		{name: "enabling after the first code does nothing", src: "1 # shellcheck enable=unused-directives\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			findings := findingsOf(tt.src)
			if got := summaries(tt.src, findings); !slices.Equal(got, tt.want) {
				t.Fatalf("%q reports %q, want %q", tt.src, got, tt.want)
			}
			if tt.fixed == "" {
				return
			}
			i := slices.IndexFunc(findings, func(f lint.Finding) bool { return f.Code == unusedDirectiveCode })
			if len(findings[i].Fixes) != 1 {
				t.Fatalf("%q offers %+v", tt.src, findings[i].Fixes)
			}
			if got := textedit.Apply(tt.src, findings[i].Fixes[0].Edits); got != tt.fixed {
				t.Errorf("%q is fixed to %q, want %q", tt.src, got, tt.fixed)
			}
		})
	}
}

func TestUnusedDirectiveFindingsWithoutOptingIn(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src string
		want      []string
	}{
		{"unused", "# shellcheck disable=SL8006\nlet a = 1; in 2\n", []string{"SL2009 SL8006"}},
		{"used", "# shellcheck disable=SL8005\nlet a = 1; in 2\n", nil},
		{"none", "let a = 1; in 2\n", nil},
		{"a broken file", "# shellcheck disable=SL8006\n{ a = ; }\n", nil},
		{"already reported by the file", "# shellcheck enable=unused-directives\n# shellcheck disable=SL8006\n1\n", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := summaries(tt.src, UnusedDirectiveFindings(tt.src)); !slices.Equal(got, tt.want) {
				t.Errorf("%q reports %q, want %q", tt.src, got, tt.want)
			}
		})
	}
	if got := UnusedDirectivesCheck(); got != "unused-directives" {
		t.Errorf("the check is called %q", got)
	}
}

func TestIgnoreFixesSilenceTheirFinding(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src string
		titles    []string
	}{
		{"a binding on its own line", "let\n  a = 1;\nin 2\n", []string{"Ignore SL8005 here", "Ignore SL8005 in this file"}},
		{"after other code on the line", "let\n  b = 1; a = b;\nin 2\n", []string{"Ignore SL8005 here", "Ignore SL8005 in this file"}},
		{"on the first line of the file", "let a = 1; in 2\n", []string{"Ignore SL8005 here", "Ignore SL8005 in this file"}},
		{"after a keyword", "let a = 1; b = 2;\nin zz + a + b\n", []string{"Ignore SL8002 here", "Ignore SL8002 in this file"}},
		{"without a final newline", "let\n  a = 1;\nin 2", []string{"Ignore SL8005 here", "Ignore SL8005 in this file"}},
		{"inside a multi-line string", "''\n  ${zz}\n''\n", []string{"Ignore SL8002 in this file"}},
		{"a line ending in a comment", "[ zz # note\n]\n", []string{"Ignore SL8002 in this file"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			findings := findingsOf(tt.src)
			if len(findings) == 0 {
				t.Fatalf("%q has no finding", tt.src)
			}
			last := findings[len(findings)-1]
			fixes := Read(tt.src).IgnoreFixes([]lint.Finding{last})[0]
			var titles []string
			for _, fix := range fixes {
				titles = append(titles, fix.Title)
				after := textedit.Apply(tt.src, fix.Edits)
				if slices.ContainsFunc(findingsOf(after), func(f lint.Finding) bool {
					return f.Code == last.Code && after[f.Span.Start:f.Span.End] == tt.src[last.Span.Start:last.Span.End]
				}) {
					t.Errorf("%q after %q is %q, which still reports %s", tt.src, fix.Title, after, last.Code)
				}
				if !strings.Contains(after, "# shellcheck disable="+last.Code.String()) {
					t.Errorf("%q after %q is %q", tt.src, fix.Title, after)
				}
			}
			if !slices.Equal(titles, tt.titles) {
				t.Errorf("%q offers %q, want %q", tt.src, titles, tt.titles)
			}
		})
	}
	syntaxError := lint.HostFinding(lint.CodeNixSyntax, findingsOf("{ a = ; }")[0].Span, "x")
	if got := Read("{ a = ; }").IgnoreFixes([]lint.Finding{syntaxError})[0]; got != nil {
		t.Errorf("a syntax error can be ignored: %+v", got)
	}
}
