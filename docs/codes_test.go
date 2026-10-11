package docs_test

import (
	"testing"

	"github.com/stubbedev/shell-lsp/docs"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

func TestEveryCheckLinksToItsOwnDocs(t *testing.T) {
	t.Parallel()

	var links []string
	for _, code := range lint.Codes() {
		links = append(links, code.Docs())
		if _, wiki := code.Wiki(); !wiki {
			links = append(links, code.Reference())
		}
	}
	docs.Resolves(t, links)
}
