package actions

import (
	"slices"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/lint"
)

func doubleBrackets(s *site) (Change, bool) {
	if !s.Tree.Dialect.Features().DoubleBracket {
		return Change{}, false
	}
	for _, node := range slices.Backward(s.pathAt()) {
		stmt, ok := node.(*syntax.Stmt)
		if !ok {
			continue
		}
		if edits, ok := lint.DoubleBrackets(s.Tree.Text(), stmt); ok {
			return Change{Title: "Convert to [[ ]]", Edits: edits}, true
		}
	}
	return Change{}, false
}
