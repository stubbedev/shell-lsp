package lint

import (
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/docs"
)

func TestNativeRulesAreDocumented(t *testing.T) {
	t.Parallel()

	for _, code := range Codes() {
		if !code.Native() {
			continue
		}
		if !strings.Contains(docs.Rules, "\n## "+code.String()+"\n") {
			t.Errorf("docs/rules.md has no section for %s", code)
			continue
		}
		if info, _ := Describe(code); info.Summary == "" {
			t.Errorf("the docs/rules.md section for %s does not open with a **bold** summary sentence", code)
		}
	}
}

func TestDescribe(t *testing.T) {
	t.Parallel()

	info, ok := Describe(codeDownloadedScript)
	if !ok || info.Severity != SeverityWarning || info.Summary != "Downloaded code runs unchecked." {
		t.Errorf("Describe(SL1005) = %+v, %t", info, ok)
	}
	if info.Reference() != "https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl1005" {
		t.Errorf("Reference() = %q", info.Reference())
	}
	optional, _ := Describe(2250)
	if optional.Optional != "require-variable-braces" || optional.Summary != "A variable is expanded without braces." || optional.Reference() != "https://www.shellcheck.net/wiki/SC2250" {
		t.Errorf("Describe(SC2250) = %+v", optional)
	}
	if _, ok := Describe(9999); ok {
		t.Error("Describe(SC9999) found a code the linter does not have")
	}
	if got := len(Catalog()); got != len(Codes()) {
		t.Errorf("Catalog() has %d entries for %d codes", got, len(Codes()))
	}
}

const maxSummaryLength = 120

func TestEveryCodeHasASummary(t *testing.T) {
	t.Parallel()

	for _, info := range Catalog() {
		switch {
		case info.Summary == "":
			t.Errorf("%s has no summary", info.Code)
		case len(info.Summary) > maxSummaryLength:
			t.Errorf("the summary of %s is %d bytes, over %d: %q", info.Code, len(info.Summary), maxSummaryLength, info.Summary)
		case !strings.HasSuffix(info.Summary, ".") || strings.ContainsRune(info.Summary, '\n'):
			t.Errorf("the summary of %s is not one sentence ending in a period: %q", info.Code, info.Summary)
		}
		if info.Code.Native() && catalog[info.Code].summary != "" {
			t.Errorf("%s has a catalog summary, but native codes take theirs from docs/rules.md", info.Code)
		}
	}
}
