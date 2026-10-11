package hover

import (
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func lintedHover(t *testing.T, src, needle string, lintEnabled bool) (Result, bool) {
	t.Helper()

	tree := parser.Parse(src, dialect.Bash)
	report := lint.Run(tree, nil, lint.Options{})
	in := Input{Tree: tree, Offset: strings.Index(src, needle) + 1, Commands: fakeCommands{docs: map[string]string{"sh": "**sh**: the shell"}}}
	in.Lint = func() (lint.Report, bool) { return report, lintEnabled }
	got, ok, err := Hover(t.Context(), in)
	if err != nil {
		t.Fatalf("Hover(%q) error = %v", needle, err)
	}
	return got, ok
}

func sectionsOf(markdown string) []string { return strings.Split(markdown, sectionSeparator) }

func TestHoverExplainsTheFindingUnderTheCursor(t *testing.T) {
	t.Parallel()

	got, ok := lintedHover(t, "#!/bin/bash\necho $1\n", " $1", true)
	sections := sectionsOf(got.Markdown)
	if !ok || len(sections) != 2 {
		t.Fatalf("hover = %+v, %t; want the SC2086 section above the parameter's", got, ok)
	}
	for _, want := range []string{
		"**SC2086** · info",
		"An unquoted expansion is split on whitespace and expanded as a glob.",
		"**Fix** (changes behaviour, review it): Double-quote $1",
		"```diff\n-echo $1\n+echo \"$1\"\n```",
		"Silence it with `# shellcheck disable=SC2086` on the line above the command.",
		"[SC2086 in the shell-lsp checks](https://github.com/stubbedev/shell-lsp/blob/main/docs/checks.md#sc2086)",
		"[ShellCheck wiki](https://www.shellcheck.net/wiki/SC2086)",
	} {
		if !strings.Contains(sections[0], want) {
			t.Errorf("SC2086 section lacks %q:\n%s", want, sections[0])
		}
	}
	if !strings.HasPrefix(sections[1], "**$1**") {
		t.Errorf("the parameter's own hover = %q, want it after the finding", sections[1])
	}
}

func TestHoverOnAFindingAloneUsesItsSpan(t *testing.T) {
	t.Parallel()

	src := "#!/bin/bash\nx=`date`\necho \"$x\"\n"
	got, ok := lintedHover(t, src, "=`", true)
	if !ok || src[got.Span.Start:got.Span.End] != "`date`" {
		t.Fatalf("hover = %+v, %t; want the SC2006 finding's span", got, ok)
	}
	for _, want := range []string{"**SC2006** · style · hidden by the `minimal` profile", "**Fix** (safe): Replace the backticks with $(...)", "-x=`date`\n+x=$(date)"} {
		if !strings.Contains(got.Markdown, want) {
			t.Errorf("SC2006 hover lacks %q:\n%s", want, got.Markdown)
		}
	}
	if strings.Contains(got.Markdown, sectionSeparator) {
		t.Errorf("a lone finding has no other hover to separate from:\n%s", got.Markdown)
	}
}

func TestHoverListsEveryOverlappingFinding(t *testing.T) {
	t.Parallel()

	got, ok := lintedHover(t, "#!/bin/bash\nx=`echo $1`\necho \"$x\"\n", " $1", true)
	sections := sectionsOf(got.Markdown)
	var codes []string
	for _, section := range sections[:len(sections)-1] {
		code, _, _ := strings.Cut(strings.TrimPrefix(section, "**"), "**")
		codes = append(codes, code)
	}
	if !ok || strings.Join(codes, ",") != "SC2006,SC2116,SC2086" || !strings.HasPrefix(sections[len(sections)-1], "**$1**") {
		t.Fatalf("hover sections %v, last %q", codes, sections[len(sections)-1])
	}
	if strings.Contains(sections[1], "**Fix**") {
		t.Errorf("SC2116 carries no fix but its section shows one:\n%s", sections[1])
	}
}

func TestHoverShowsANativeRuleWithItsExample(t *testing.T) {
	t.Parallel()

	got, ok := lintedHover(t, "#!/bin/bash\ncurl -fsSL https://example.com/i.sh | sh\n", " sh\n", true)
	for _, want := range []string{
		"**SL1005** · warning",
		"Downloaded code runs unchecked.",
		"For example:\n\n```bash\ncurl -fsSL https://example.com/install.sh | sh    # SL1005\n",
		"[SL1005 in the shell-lsp rules](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl1005)",
	} {
		if !ok || !strings.Contains(got.Markdown, want) {
			t.Errorf("SL1005 hover lacks %q:\n%s", want, got.Markdown)
		}
	}
	if strings.Contains(got.Markdown, "ShellCheck wiki") {
		t.Errorf("SL1005 has no ShellCheck wiki page:\n%s", got.Markdown)
	}
	if sections := sectionsOf(got.Markdown); sections[len(sections)-1] != "**sh**: the shell" {
		t.Errorf("the command's own hover = %q, want it after the finding", sections[len(sections)-1])
	}
}

func TestHoverWithoutFindingsIsUnchanged(t *testing.T) {
	t.Parallel()

	quoted, _ := lintedHover(t, "#!/bin/bash\necho \"$1\"\n", "\"$1", true)
	off, _ := lintedHover(t, "#!/bin/bash\necho $1\n", " $1", false)
	for name, got := range map[string]Result{"no finding": quoted, "lint off": off} {
		if strings.Contains(got.Markdown, "Silence it") || !strings.HasPrefix(got.Markdown, "**$1**") {
			t.Errorf("%s: hover = %q, want only the parameter", name, got.Markdown)
		}
	}
}

func TestChangedLinesShowsOnlyShortFixes(t *testing.T) {
	t.Parallel()

	text := "a\nb\nc\nd\ne\nf\ng\nh\n"
	before, after, ok := changedLines(text, []textedit.Edit{{Start: 2, End: 3, NewText: "B"}, {Start: 6, End: 7, NewText: "D"}})
	if !ok || strings.Join(before, "|") != "b|c|d" || strings.Join(after, "|") != "B|c|D" {
		t.Errorf("changedLines = %q, %q, %t", before, after, ok)
	}
	if diff := lineDiff(before, after); diff != "-b\n-c\n-d\n+B\n+c\n+D" {
		t.Errorf("lineDiff = %q", diff)
	}
	if _, _, ok := changedLines(text, []textedit.Edit{{Start: 0, End: 15, NewText: ""}}); ok {
		t.Error("a fix spanning more lines than the preview shows was previewed")
	}
	if diff := lineDiff([]string{"keep", "old", "tail"}, []string{"keep", "new", "tail"}); diff != " keep\n-old\n+new\n tail" {
		t.Errorf("lineDiff with context = %q", diff)
	}
}

func TestFencedOutrunsBackticksInTheBody(t *testing.T) {
	t.Parallel()

	if got := fenced("diff", "-a ```b```"); got != "````diff\n-a ```b```\n````" {
		t.Errorf("fenced = %q", got)
	}
}
