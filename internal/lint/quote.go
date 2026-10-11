package lint

import (
	"slices"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/parser"
)

type expansionContext uint8

const (
	contextQuoted expansionContext = iota
	contextSplit
	contextCommandName
	contextArrayElement
	contextLoopItems
)

func (p *pass) contextOf(part syntax.WordPart) expansionContext {
	word, ok := p.parent().(*syntax.Word)
	if !ok {
		return contextQuoted
	}
	switch owner := p.ancestor(1).(type) {
	case *syntax.CallExpr:
		if p.effectiveCommandWord(owner) == word && len(word.Parts) == 1 && word.Parts[0] == part {
			return contextCommandName
		}
		if slices.Contains(owner.Args, word) {
			return contextSplit
		}
		return contextQuoted
	case *syntax.Redirect:
		if owner.Word == word {
			return contextSplit
		}
	case *syntax.Assign:
		if owner.Naked && owner.Name == nil {
			return contextSplit
		}
		if _, declared := p.ancestor(2).(*syntax.DeclClause); declared && !p.dialect.Features().DeclarationAssigns {
			return contextSplit
		}
	case *syntax.ArrayElem:
		if owner.Index != nil {
			return contextQuoted
		}
		return contextArrayElement
	case *syntax.WordIter:
		return contextLoopItems
	case *syntax.BraceExp:
		return p.braceContext()
	}
	return contextQuoted
}

func (p *pass) enclosingShellWord() (*syntax.Word, bool) {
	word, ok := p.parent().(*syntax.Word)
	if !ok {
		return nil, false
	}
	switch owner := p.ancestor(1).(type) {
	case *syntax.CallExpr, *syntax.CaseClause, *syntax.CaseItem, *syntax.WordIter,
		*syntax.TestClause, *syntax.UnaryTest, *syntax.ParenTest:
		return word, true
	case *syntax.BinaryTest:
		return word, owner.Op != syntax.TsReMatch || owner.X == word
	case *syntax.Assign:
		return word, owner.Value == word
	case *syntax.ArrayElem:
		return word, owner.Value == word
	case *syntax.Redirect:
		return word, owner.Word == word && !parser.IsHeredoc(owner.Op)
	}
	return nil, false
}

func (p *pass) braceContext() expansionContext {
	for depth := 2; ; depth++ {
		switch p.ancestor(depth).(type) {
		case *syntax.BraceExp, *syntax.Word:
			continue
		case *syntax.CallExpr, *syntax.Redirect:
			return contextSplit
		}
		return contextQuoted
	}
}

type expansionKind uint8

const (
	expandsScalar expansionKind = iota
	expandsElements
	expandsJoined
)

func kindOf(pe *syntax.ParamExp) expansionKind {
	if pe.Param == nil || pe.Length {
		return expandsScalar
	}
	all := pe.Param.Value
	if pe.Index != nil {
		w, ok := pe.Index.(*syntax.Word)
		if !ok {
			return expandsScalar
		}
		all = w.Lit()
	}
	switch all {
	case "@":
		return expandsElements
	case "*":
		if pe.Excl {
			return expandsScalar
		}
		return expandsJoined
	}
	return expandsScalar
}

func isAlternativeExpansion(pe *syntax.ParamExp) bool {
	return pe.Exp != nil && (pe.Exp.Op == syntax.AlternateUnset || pe.Exp.Op == syntax.AlternateUnsetOrNull)
}

func assignsParameter(pe *syntax.ParamExp) bool {
	if pe.Exp == nil {
		return false
	}
	switch pe.Exp.Op {
	case syntax.AssignUnset, syntax.AssignUnsetOrNull, syntax.AssignAlways:
		return true
	}
	return false
}

func (p *pass) parsesGlobs(w *syntax.Word) bool {
	switch parent := p.parent().(type) {
	case *syntax.CallExpr, *syntax.CaseItem, *syntax.WordIter:
		return true
	case *syntax.BinaryTest:
		return parent.Op != syntax.TsReMatch
	case *syntax.Assign:
		return parent.Value == w
	case *syntax.ArrayElem:
		return parent.Value == w
	case *syntax.Redirect:
		return parent.Word == w
	}
	return false
}
