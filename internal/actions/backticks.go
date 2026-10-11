package actions

import (
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

func convertBackticks(s *site) (Change, bool) {
	path := s.pathAt()
	for i, node := range path {
		cs, ok := node.(*syntax.CmdSubst)
		if !ok || !cs.Backquotes {
			continue
		}
		replacement, ok := parser.DollarSubstitution(s.Tree.Text(), cs, path[:i])
		if !ok {
			return Change{}, false
		}
		span, _ := s.Tree.NodeSpan(cs)
		return Change{Title: "Convert backticks to $(...)", Edits: []textedit.Edit{{Start: span.Start, End: span.End, NewText: replacement}}}, true
	}
	return Change{}, false
}
