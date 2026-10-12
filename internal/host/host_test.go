package host

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

func TestOf(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		uri, languageID string
		want            Language
		ok              bool
	}{
		{"file:///w/Makefile", "makefile", Make, true},
		{"file:///w/rules", "make", Make, true},
		{"file:///w/Makefile", "shellscript", 0, false},
		{"file:///w/Makefile", "", Make, true},
		{"file:///w/GNUmakefile", "", Make, true},
		{"file:///w/build/common.mk", "", Make, true},
		{`C:\w\rules.mak`, "", Make, true},
		{"file:///w/Makefile.sh", "", 0, false},
		{"file:///w/run.sh", "", 0, false},
		{"file:///w/justfile", "just", Just, true},
		{"file:///w/Justfile", "", Just, true},
		{"file:///w/.justfile", "", Just, true},
		{"file:///w/tasks.just", "", Just, true},
		{"file:///w/flake.nix", "", Nix, true},
		{"file:///w/x", "nix", Nix, true},
	} {
		got, ok := Of(tt.uri, tt.languageID)
		if ok != tt.ok || ok && got != tt.want {
			t.Errorf("Of(%q, %q) = %v, %v, want %v, %v", tt.uri, tt.languageID, got, ok, tt.want, tt.ok)
		}
	}
}

func TestEveryLanguageIsDefined(t *testing.T) {
	t.Parallel()

	for _, l := range []Language{Make, Just, Nix} {
		if d := definitions[l]; d.name == "" || len(d.languageIDs) == 0 || d.project == nil {
			t.Errorf("language %d is not fully defined: %+v", l, d)
		}
	}
}

func TestParseKeepsEachRecipeLineToItself(t *testing.T) {
	t.Parallel()

	text := "a:\n\techo \"unclosed\nb:\n\tif true; then echo x\nc:\n\techo $$(date\nd:\n\techo == x\n"
	script := Make.Project(docstore.NewDocument("file:///w/Makefile", "makefile", 1, text))
	tree := script.Parse(nil)
	type located struct {
		line, character uint32
		message         string
	}
	var got []located
	for _, e := range tree.Errors {
		at := script.Doc.OffsetToPosition(e.Span.Start, docstore.EncodingUTF16)
		got = append(got, located{at.Line, at.Character, e.Message})
	}
	want := []located{
		{1, 6, "missing `\"` to match `\"`"},
		{3, 1, "missing `fi` to match `if`"},
		{5, 6, "missing `)` to match `$(`"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("errors\n got %+v\nwant %+v", got, want)
	}
	if at := script.Doc.OffsetToPosition(len(script.Doc.Text()), docstore.EncodingUTF16); at != (lsp.Position{Line: 7, Character: 10}) {
		t.Errorf("the end of the projection is at %+v", at)
	}
}

func TestLostChangesBlameTheShellOfTheLine(t *testing.T) {
	t.Parallel()

	text := "x:\n\tFOO=bar\n\techo \"$$FOO\"\n\t(BAR=1); echo \"$$BAR\"\n"
	script := Make.Project(docstore.NewDocument("file:///w/Makefile", "makefile", 1, text))
	report := lint.Run(script.Parse(nil), nil, lint.Options{DialectSource: lint.DialectForced})
	var got []string
	for _, f := range report.Findings {
		if f.Code == 2030 || f.Code == 2031 {
			got = append(got, f.Message)
		}
	}
	want := []string{
		"This change to FOO is made in a line or script that runs in a shell of its own, so nothing after it sees the change.",
		"FOO was changed on line 2, which runs in a shell of its own, so this still sees the value from before that change.",
		"This change to BAR happens in a subshell caused by a ( ) group, so it is gone once that subshell exits.",
		"BAR was changed in a subshell (a ( ) group on line 4), so this still sees the value from before that change.",
	}
	if !slices.Equal(got, want) {
		t.Errorf("messages\n got %q\nwant %q", got, want)
	}
}

func TestShellFlagsReachTheLinter(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		makefile string
		warned   bool
	}{
		{"x:\n\tcd sub\n", true},
		{".SHELLFLAGS := -ec\nx:\n\tcd sub\n", false},
	} {
		script := Make.Project(docstore.NewDocument("file:///w/Makefile", "makefile", 1, tt.makefile))
		report := lint.Run(script.Parse(nil), nil, lint.Options{DialectSource: lint.DialectForced})
		warned := slices.ContainsFunc(report.Findings, func(f lint.Finding) bool { return f.Code == 2164 })
		if warned != tt.warned {
			t.Errorf("%q: SC2164 reported = %v, want %v", tt.makefile, warned, tt.warned)
		}
	}
}
