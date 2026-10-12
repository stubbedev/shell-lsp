package lint

import (
	"strings"
	"sync"

	"github.com/stubbedev/shell-lsp/docs"
)

type CodeInfo struct {
	Code     Code
	Severity Severity
	Optional string
	Summary  string
	Minimal  bool
	Example  string
}

func Describe(code Code) (CodeInfo, bool) {
	m, ok := catalog[code]
	if !ok {
		return CodeInfo{}, false
	}
	info := CodeInfo{Code: code, Severity: m.severity, Optional: m.optional.String(), Summary: m.summary, Minimal: minimalKeeps(m.severity, m.bug)}
	if code.Native() {
		doc := nativeDocs()[code]
		info.Summary, info.Example = doc.summary, doc.example
	}
	return info, true
}

func Catalog() []CodeInfo {
	codes := Codes()
	out := make([]CodeInfo, len(codes))
	for i, code := range codes {
		out[i], _ = Describe(code)
	}
	return out
}

const (
	docsURL           = "https://github.com/stubbedev/shell-lsp/blob/main/docs/"
	checksPage        = "checks.md"
	rulesPage         = "rules.md"
	shellcheckWikiURL = "https://www.shellcheck.net/wiki/"
)

func (c Code) Anchor() string { return strings.ToLower(c.String()) }

func (c Code) Docs() string {
	if c.Native() {
		return docsURL + rulesPage + "#" + c.Anchor()
	}
	return docsURL + checksPage + "#" + c.Anchor()
}

func (c Code) Wiki() (string, bool) {
	if c.Native() {
		return "", false
	}
	return shellcheckWikiURL + c.String(), true
}

func (c Code) Reference() string {
	if wiki, ok := c.Wiki(); ok {
		return wiki
	}
	return c.Docs()
}

const (
	sectionPrefix = "\n## "
	bold          = "**"
	exampleOpen   = "```bash\n"
	fence         = "```"
)

type nativeDoc struct {
	summary string
	example string
}

var nativeDocs = sync.OnceValue(func() map[Code]nativeDoc {
	out := map[Code]nativeDoc{}
	for _, section := range strings.Split(docs.Rules, sectionPrefix)[1:] {
		heading, body, _ := strings.Cut(section, "\n")
		code, err := ParseCode(heading)
		if err != nil || !code.Native() {
			continue
		}
		summary, ok := strings.CutPrefix(strings.TrimSpace(body), bold)
		if !ok {
			continue
		}
		text, _, closed := strings.Cut(summary, bold)
		if !closed {
			continue
		}
		doc := nativeDoc{summary: strings.TrimSpace(text)}
		if _, block, found := strings.Cut(body, exampleOpen); found {
			doc.example, _, _ = strings.Cut(block, fence)
			doc.example = strings.TrimSuffix(doc.example, "\n")
		}
		out[code] = doc
	}
	return out
})
