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
}

func Describe(code Code) (CodeInfo, bool) {
	m, ok := catalog[code]
	if !ok {
		return CodeInfo{}, false
	}
	return CodeInfo{Code: code, Severity: m.severity, Optional: m.optional, Summary: summaryOf(code, m)}, true
}

func summaryOf(code Code, m meta) string {
	if code.Native() {
		return nativeSummaries()[code]
	}
	return m.summary
}

func Catalog() []CodeInfo {
	codes := Codes()
	out := make([]CodeInfo, len(codes))
	for i, code := range codes {
		out[i], _ = Describe(code)
	}
	return out
}

func (c CodeInfo) Reference() string {
	if c.Code.Native() {
		return "https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#" + strings.ToLower(c.Code.String())
	}
	return "https://www.shellcheck.net/wiki/" + c.Code.String()
}

const (
	sectionPrefix = "\n## "
	bold          = "**"
)

var nativeSummaries = sync.OnceValue(func() map[Code]string {
	out := map[Code]string{}
	for _, section := range strings.Split(docs.Rules, sectionPrefix)[1:] {
		heading, body, _ := strings.Cut(section, "\n")
		code, err := ParseCode(heading)
		if err != nil || !code.Native() {
			continue
		}
		body = strings.TrimSpace(body)
		summary, ok := strings.CutPrefix(body, bold)
		if !ok {
			continue
		}
		if text, _, closed := strings.Cut(summary, bold); closed {
			out[code] = strings.TrimSpace(text)
		}
	}
	return out
})
