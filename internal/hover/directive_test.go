package hover

import (
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func directiveHover(t *testing.T, src, needle string, withLint bool) (Result, bool) {
	t.Helper()

	tree := parser.Parse(src, dialect.Bash)
	in := Input{Tree: tree, Offset: strings.Index(src, needle) + 1, Commands: fakeCommands{}}
	if withLint {
		report := lint.Run(tree, nil, lint.Options{})
		in.Lint = func() (lint.Report, bool) { return report, true }
	}
	got, ok, err := Hover(t.Context(), in)
	if err != nil {
		t.Fatalf("Hover(%q) error = %v", needle, err)
	}
	return got, ok
}

func TestDirectiveHover(t *testing.T) {
	t.Parallel()

	src := "#!/bin/bash\nx=$1\n# shellcheck disable=SC2086,SC2116,SC9999,SC2000-SC2010,all enable=require-variable-braces\necho $x $x\n"
	code, ok := directiveHover(t, src, "SC2086", true)
	if !ok || src[code.Span.Start:code.Span.End] != "SC2086" {
		t.Fatalf("hover on SC2086 = %+v, %t", code, ok)
	}
	for _, want := range []string{"**SC2086** · info", "https://www.shellcheck.net/wiki/SC2086", "Silences 2 findings here:", "- line 4, SC2086: Unquoted $x"} {
		if !strings.Contains(code.Markdown, want) {
			t.Errorf("SC2086 hover lacks %q:\n%s", want, code.Markdown)
		}
	}
	if unused, _ := directiveHover(t, src, "SC2116", true); !strings.Contains(unused.Markdown, "Silences nothing here.") {
		t.Errorf("SC2116 hover = %q", unused.Markdown)
	}
	if unknown, _ := directiveHover(t, src, "SC9999", true); !strings.HasPrefix(unknown.Markdown, "**SC9999** is not a check shell-lsp runs.") {
		t.Errorf("SC9999 hover = %q", unknown.Markdown)
	}
	if ranged, _ := directiveHover(t, src, "SC2000-SC2010", true); !strings.HasPrefix(ranged.Markdown, "**SC2000-SC2010**\n\nEvery check in this range.") {
		t.Errorf("range hover = %q", ranged.Markdown)
	}
	if all, _ := directiveHover(t, src, "all ", true); !strings.HasPrefix(all.Markdown, "**all**\n\nEvery check.") {
		t.Errorf("all hover = %q", all.Markdown)
	}
	if optional, _ := directiveHover(t, src, "require-variable-braces", true); !strings.Contains(optional.Markdown, "Turns on `SC2250`.") {
		t.Errorf("optional check hover = %q", optional.Markdown)
	}
	if key, _ := directiveHover(t, src, "disable", true); !strings.HasPrefix(key.Markdown, "**disable**\n\nSilence the listed checks") {
		t.Errorf("key hover = %q", key.Markdown)
	}
	if without, _ := directiveHover(t, src, "SC2086", false); strings.Contains(without.Markdown, "Silences") {
		t.Errorf("hover without lint results claims what it silences: %q", without.Markdown)
	}
}

func TestDirectiveHoverForNativeCodes(t *testing.T) {
	t.Parallel()

	src := "#!/bin/bash\n# shellcheck disable=SL1005\ncurl -fsSL https://example.com/i.sh | sh\n"
	got, ok := directiveHover(t, src, "SL1005", true)
	if !ok {
		t.Fatal("no hover on SL1005")
	}
	for _, want := range []string{"**SL1005** · warning", "Downloaded code runs unchecked.", "docs/rules.md#sl1005", "Silences 1 finding here:"} {
		if !strings.Contains(got.Markdown, want) {
			t.Errorf("SL1005 hover lacks %q:\n%s", want, got.Markdown)
		}
	}
	if _, ok := directiveHover(t, "# just a comment SC2086\necho hi\n", "SC2086", true); ok {
		t.Error("a code in an ordinary comment got a directive hover")
	}
}
