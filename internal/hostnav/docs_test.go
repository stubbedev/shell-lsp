package hostnav

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/docs"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

var documentedCode = regexp.MustCompile(`#\s+(SL\d{4})\b`)

func TestRulesDocHostExamplesReportTheirCodes(t *testing.T) {
	t.Parallel()

	files := map[string]string{"just": "justfile", "make": "Makefile"}
	examples := 0
	for _, section := range strings.Split(docs.Rules, "\n## ")[1:] {
		heading, body, _ := strings.Cut(section, "\n")
		code, err := lint.ParseCode(heading)
		if err != nil || !code.Host() {
			continue
		}
		language := "just"
		if code.Make() {
			language = "make"
		}
		blocks := strings.Split(body, "```"+language+"\n")[1:]
		if len(blocks) == 0 {
			t.Errorf("%s has no ```%s example", heading, language)
		}
		for i, block := range blocks {
			examples++
			example, _, _ := strings.Cut(block, "```")
			found := map[int][]string{}
			for _, f := range findingsAlone(t, files[language])(example) {
				line := strings.Count(example[:f.Span.Start], "\n") + 1
				found[line] = append(found[line], f.Code.String())
			}
			for n, line := range strings.Split(example, "\n") {
				if m := documentedCode.FindStringSubmatch(line); m != nil && !slices.Contains(found[n+1], m[1]) {
					t.Errorf("%s example %d line %d %q reports %v, not %s", heading, i+1, n+1, line, found[n+1], m[1])
				}
			}
		}
	}
	if examples == 0 {
		t.Fatal("docs/rules.md has no justfile or Makefile examples")
	}
}
