package lint

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/docs"
	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/parser"
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

var (
	documentedCode = regexp.MustCompile(`#\s+(SL\d{4})\b`)
	documentedSafe = regexp.MustCompile(`#\s+(?:safe|validated)\b`)
)

func TestRulesDocExamplesReportTheirCodes(t *testing.T) {
	t.Parallel()

	for _, section := range strings.Split(docs.Rules, "\n## ")[1:] {
		heading, body, _ := strings.Cut(section, "\n")
		for i, block := range strings.Split(body, "```bash\n")[1:] {
			example, _, _ := strings.Cut(block, "```")
			d := dialect.Detect(nil, dialect.Input{URI: "file:///example.sh", Text: example})
			tree := parser.Parse(example, d)
			found := map[int][]string{}
			for _, f := range Check(tree, nil, Options{DialectSource: DialectDetected, Optional: []string{directive.All}}) {
				line := tree.Line(f.Span.Start)
				found[line] = append(found[line], f.Code.String())
			}
			for n, line := range strings.Split(example, "\n") {
				reported := found[n+1]
				if m := documentedCode.FindStringSubmatch(line); m != nil && !slices.Contains(reported, m[1]) {
					t.Errorf("%s example %d line %d %q reports %v, not %s", heading, i+1, n+1, line, reported, m[1])
				}
				if documentedSafe.MatchString(line) && slices.Contains(reported, heading) {
					t.Errorf("%s example %d line %d %q is documented as safe but reports %s", heading, i+1, n+1, line, heading)
				}
			}
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
