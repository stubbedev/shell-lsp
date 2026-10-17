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

type docLanguage struct {
	fence, file string
	marks       func(example string) map[int]string
}

func trailingMarks(example string) map[int]string {
	out := map[int]string{}
	for n, line := range strings.Split(example, "\n") {
		if m := documentedCode.FindStringSubmatch(line); m != nil {
			out[n+1] = m[1]
		}
	}
	return out
}

func precedingMarks(example string) map[int]string {
	out := map[int]string{}
	lines := strings.Split(example, "\n")
	for n, line := range lines {
		m := documentedCode.FindStringSubmatch(line)
		if m == nil || !strings.HasPrefix(line, "#") {
			continue
		}
		next := n + 1
		for next < len(lines) && strings.HasPrefix(lines[next], "#") {
			next++
		}
		out[next+1] = m[1]
	}
	return out
}

func docLanguageOf(code lint.Code) docLanguage {
	switch {
	case code.Just():
		return docLanguage{fence: "just", file: "justfile", marks: trailingMarks}
	case code.Make():
		return docLanguage{fence: "make", file: "Makefile", marks: trailingMarks}
	}
	return docLanguage{fence: "dockerfile", file: "Dockerfile", marks: precedingMarks}
}

func TestRulesDocHostExamplesReportTheirCodes(t *testing.T) {
	t.Parallel()

	examples := 0
	for _, section := range strings.Split(docs.Rules, "\n## ")[1:] {
		heading, body, _ := strings.Cut(section, "\n")
		code, err := lint.ParseCode(heading)
		if err != nil || !code.Host() {
			continue
		}
		language := docLanguageOf(code)
		blocks := strings.Split(body, "```"+language.fence+"\n")[1:]
		if len(blocks) == 0 {
			t.Errorf("%s has no ```%s example", heading, language.fence)
		}
		for i, block := range blocks {
			examples++
			example, _, _ := strings.Cut(block, "```")
			found := map[int][]string{}
			for _, f := range findingsAlone(t, language.file)(example) {
				line := strings.Count(example[:f.Span.Start], "\n") + 1
				found[line] = append(found[line], f.Code.String())
			}
			marks := language.marks(example)
			if len(marks) == 0 {
				t.Errorf("%s example %d marks no line with its code", heading, i+1)
			}
			for line, want := range marks {
				if !slices.Contains(found[line], want) {
					t.Errorf("%s example %d line %d reports %v, not %s", heading, i+1, line, found[line], want)
				}
			}
		}
	}
	if examples == 0 {
		t.Fatal("docs/rules.md has no host examples")
	}
}
