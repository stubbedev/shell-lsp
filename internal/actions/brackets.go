package actions

import (
	"slices"

	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

func doubleBrackets(s *site) (Change, bool) {
	if !s.Tree.Dialect.Features().DoubleBracket {
		return Change{}, false
	}
	for _, located := range slices.Backward(s.pathAt()) {
		stmt, ok := located.Node.(*syntax.Stmt)
		if !ok {
			continue
		}
		if edits, ok := lint.DoubleBrackets(s.Tree.Text(), stmt); ok {
			return Change{Title: "Convert to [[ ]]", Edits: edits}, true
		}
	}
	return Change{}, false
}
