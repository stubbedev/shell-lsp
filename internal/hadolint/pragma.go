package hadolint

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/dockerfile"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

func lineAfter(text string, offset int) int {
	end := strings.IndexByte(text[offset:], '\n')
	if end < 0 {
		return len(text)
	}
	return offset + end + 1
}

func lineStart(text string, offset int) int { return strings.LastIndexByte(text[:offset], '\n') + 1 }

func (c *checker) pragmas() {
	if !c.enabled(lint.DL1001) {
		return
	}
	for _, p := range c.File.Pragmas() {
		if p.Kind == dockerfile.PragmaIgnore {
			c.report(lint.DL1001, p.Span, "this comment hides findings from %s; fix them, or ignore them in .hadolint.yaml", codeNames(p.Codes))
		}
	}
}

func codeNames(codes []lint.Code) string {
	names := make([]string, len(codes))
	for i, c := range codes {
		names[i] = c.String()
	}
	return strings.Join(names, ", ")
}
