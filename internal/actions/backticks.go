package actions

import (
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

func convertBackticks(s *site) (Change, bool) {
	path := s.pathAt()
	ancestors := make([]syntax.Node, 0, len(path))
	for _, located := range path {
		cs, ok := located.Node.(*syntax.CmdSubst)
		if !ok || !cs.Backquotes {
			ancestors = append(ancestors, located.Node)
			continue
		}
		replacement, ok := parser.DollarSubstitution(s.Tree.Text(), cs, ancestors)
		if !ok {
			return Change{}, false
		}
		span := located.Span
		return Change{Title: "Convert backticks to $(...)", Edits: []textedit.Edit{{Start: span.Start, End: span.End, NewText: replacement}}}, true
	}
	return Change{}, false
}
