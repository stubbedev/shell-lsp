package main

import (
	"os"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

func sampleDifferences() []difference {
	return []difference{
		{id: "a/x.sh", shell: dashShell, verdict: shellRejects, detail: "dash: 1: Syntax error"},
		{id: "oils/spec/b.test.sh#3", shell: zshShell, verdict: parserRejects, detail: "line 1: oops"},
	}
}

func TestShellsBaselineRoundTripsAndKeepsCategories(t *testing.T) {
	t.Parallel()

	got := sampleDifferences()
	b := reclassify(shellsBaseline{}, 10, got)
	for _, e := range b.entries {
		if e.category != unclassified {
			t.Errorf("a new difference got category %s", e.category)
		}
	}
	b.entries[0].category = posixExtension
	b.entries[1].category = parserGap
	parsed, err := parseShellsBaseline(renderShellsBaseline(b))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.parses != 10 || len(parsed.entries) != 2 || parsed.entries[0].category != posixExtension || parsed.entries[1].key() != got[1].key() {
		t.Errorf("parsed %+v", parsed)
	}
	again := reclassify(parsed, 10, got)
	if again.entries[0].category != posixExtension || again.entries[1].category != parserGap {
		t.Errorf("refreshing lost the categories: %+v", again.entries)
	}
	if problems := diffShells(again, 10, got); len(problems) != 0 {
		t.Errorf("an up-to-date baseline reports %v", problems)
	}
}

func TestDiffShellsReportsEveryKindOfChange(t *testing.T) {
	t.Parallel()

	got := sampleDifferences()
	want := reclassify(shellsBaseline{}, 10, got[:1])
	want.entries = append(want.entries, classified{id: "gone.sh", shell: bashShell, verdict: parserRejects, category: parserGap})
	problems := diffShells(want, 11, got)
	joined := strings.Join(problems, "\n")
	for _, part := range []string{"10 parses, now 11", "unclassified: dash shell-rejects a/x.sh", "NEW: zsh parser-rejects oils/spec/b.test.sh#3: line 1: oops", "gone, refresh the baseline: bash parser-rejects gone.sh"} {
		if !strings.Contains(joined, part) {
			t.Errorf("problems lack %q:\n%s", part, joined)
		}
	}
}

func TestParseShellsBaselineRejectsUnknownNames(t *testing.T) {
	t.Parallel()

	for _, line := range []string{
		"no-such-category dash shell-rejects a.sh",
		"parser-gap fish shell-rejects a.sh",
		"parser-gap dash both-reject a.sh",
		"parser-gap dash",
	} {
		if _, err := parseShellsBaseline(shellsCountPrefix + "1\n" + line + "\n"); err == nil {
			t.Errorf("%q parsed", line)
		}
	}
}

func TestEveryDialectHasARealShell(t *testing.T) {
	t.Parallel()

	for d := range dialect.All.Members() {
		shell, ok := realShellFor(dialect.Shell{Dialect: d})
		if !ok {
			t.Errorf("%s has no real shell", d)
			continue
		}
		if got := realShells[shell].ours.Dialect; got != d && d != dialect.Posix {
			t.Errorf("%s is checked with %s, which parses as %s", d, shell, got)
		}
	}
	if _, ok := realShellFor(dialect.FlavorBats.Shell()); ok {
		t.Error("Bats files have a real shell")
	}
	if shell, _ := realShellFor(dialect.FlavorMksh.Shell()); shell != mkshShell {
		t.Errorf("mksh scripts are checked with %s", shell)
	}
	for _, name := range []string{"bash", "dash", "zsh", "mksh", "ash"} {
		if _, ok := oilsShell(name); !ok {
			t.Errorf("the Oils shell %s has no real shell", name)
		}
	}
	if _, ok := oilsShell("osh"); ok {
		t.Error("osh maps to a real shell")
	}
}

func TestCorpusDocDescribesEveryCategory(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../docs/corpus.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range categoryNames {
		if !strings.Contains(string(raw), "`"+name+"`") {
			t.Errorf("docs/corpus.md does not describe the category %s", name)
		}
	}
}
