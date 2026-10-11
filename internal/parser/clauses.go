package parser

import "github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"

func LoopNames(iter *syntax.WordIter) []*syntax.Lit {
	if iter.Name != nil {
		return []*syntax.Lit{iter.Name}
	}
	return iter.Names
}

func ListsWords(iter *syntax.WordIter) bool {
	return iter.InPos.IsValid() || iter.Lparen.IsValid()
}

func IsElse(clause *syntax.IfClause) bool {
	return !clause.ThenPos.IsValid() && !clause.ThenPos.IsRecovered() && !clause.Short
}

func loopDelimiters(braces bool) (string, string) {
	if braces {
		return "{", "}"
	}
	return "do", "done"
}
