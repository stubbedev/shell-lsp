package lint

import (
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const codeUnknownTransformation = nativeBase + 2002

var expansionsFamily = family{
	codes: map[Code]meta{
		codeUnknownTransformation: {severity: SeverityWarning},
		2231:                      {severity: SeverityInfo, bug: true, summary: "An unquoted expansion next to a glob is split and globbed along with it."},
		2247:                      {severity: SeverityWarning, summary: "$\" starts a translated string where a quoted substitution was meant."},
		2256:                      {severity: SeverityWarning, summary: "$\"name\" is a translated string, not an expansion of the variable."},
		2295:                      {severity: SeverityInfo, bug: true, summary: "An unquoted expansion inside a pattern is matched as a glob."},
		2302:                      {severity: SeverityWarning, summary: "A loop iterates over an associative array's values but uses the loop variable as a key."},
		2303:                      {severity: SeverityWarning, summary: "A value taken from an associative array is used as a key."},
		2321:                      {severity: SeverityStyle, summary: "$(( )) around an array index is redundant, since indices are already arithmetic."},
		2322:                      {severity: SeverityStyle, summary: "Doubled parentheses in arithmetic are redundant."},
		2323:                      {severity: SeverityStyle, summary: "Parentheses around a whole arithmetic expression are redundant."},
	},
	nodes: []nodeRule{
		onNode(checkLoopGlob),
		onNode(checkTranslatedString),
		onNode(checkRemovalPattern),
		onNode(checkValueAsKey),
		onNode(checkUnknownTransformation),
		onNode(checkArithmeticIndex),
		onNodes(checkRedundantParentheses, &syntax.ParenArithm{}, &syntax.ArithmExp{}, &syntax.ArithmCmd{}, &syntax.CStyleLoop{}, &syntax.Assign{}),
	},
}

var (
	bashTransformations = setOf("U", "u", "L", "Q", "E", "P", "A", "K", "a", "k")
	mkshTransformations = setOf("Q", "#")
)

func checkUnknownTransformation(p *pass, pe *syntax.ParamExp) {
	if pe.Param == nil || pe.Exp == nil || pe.Exp.Op != syntax.OtherParamOps || pe.Exp.Word == nil {
		return
	}
	known, names := bashTransformations, "U, u, L, Q, E, P, A, K, a and k"
	if p.tree.Lang() == syntax.LangMirBSDKorn {
		known, names = mkshTransformations, "Q and #"
	}
	op, literal := parser.Literal(pe.Exp.Word)
	if !literal || known[op] {
		return
	}
	if span, ok := p.span(pe.Exp.Word); ok {
		p.report(codeUnknownTransformation, span, "@"+op+" is not a transformation the shell knows, so expanding "+pe.Param.Value+" fails with \"bad substitution\" whenever it is set; the operators are "+names+".")
	}
}

func checkLoopGlob(p *pass, iter *syntax.WordIter) {
	for _, w := range iter.Items {
		if !isGlob(w) {
			continue
		}
		for _, part := range w.Parts {
			var split, globbed bool
			switch part := part.(type) {
			case *syntax.ParamExp:
				split, globbed = p.implicitlySplit(part), p.implicitlyGlobbed(part)
			case *syntax.CmdSubst:
				split, globbed = true, p.substitutionGlobbed()
			}
			if split || globbed {
				if span, ok := p.span(part); ok {
					p.report(2231, span, "Quote this expansion so only the glob around it is expanded; its own value would otherwise be "+expansionFate(split, globbed)+" too.", p.quoteFix(span, Review))
				}
			}
		}
	}
}

func expansionFate(split, globbed bool) string {
	switch {
	case split && globbed:
		return "split and globbed"
	case split:
		return "split"
	}
	return "globbed"
}

func checkTranslatedString(p *pass, dq *syntax.DblQuoted) {
	if !dq.Dollar || len(dq.Parts) == 0 {
		return
	}
	lit, ok := dq.Parts[0].(*syntax.Lit)
	if !ok || lit.Value == "" {
		return
	}
	span, ok := p.span(dq)
	if !ok {
		return
	}
	swap := reviewFix("Move the $ inside the quotes", textedit.Edit{Start: span.Start, End: span.Start + 2, NewText: `"$`})
	switch {
	case lit.Value[0] == '(' || lit.Value[0] == '{':
		p.report(2247, span, `$" starts a translated string; to quote a substitution, write "$`+lit.Value[:1]+` instead.`, swap)
	case len(dq.Parts) == 1 && parser.IsName(lit.Value) && p.assigned(lit.Value):
		p.report(2256, span, `$"`+lit.Value+`" is a translated string, not the variable `+lit.Value+`; write "$`+lit.Value+`" to expand it.`, swap)
	}
}

func (p *pass) assigned(name string) bool {
	for _, def := range p.tree.Definitions {
		if def.Name == name {
			return true
		}
	}
	return false
}

func checkRemovalPattern(p *pass, pe *syntax.ParamExp) {
	if pe.Short || pe.Index != nil || pe.Exp == nil || pe.Exp.Word == nil {
		return
	}
	switch pe.Exp.Op {
	case syntax.RemSmallPrefix, syntax.RemLargePrefix, syntax.RemSmallSuffix, syntax.RemLargeSuffix:
	default:
		return
	}
	for _, part := range pe.Exp.Word.Parts {
		switch part := part.(type) {
		case *syntax.ParamExp:
			if p.implicitlyGlobbed(part) {
				p.reportLiteralPattern(part)
			}
		case *syntax.CmdSubst:
			if !part.TempFile && !part.ReplyVar && p.substitutionGlobbed() {
				p.reportLiteralPattern(part)
			}
		}
	}
}

func (p *pass) reportLiteralPattern(part syntax.WordPart) {
	if span, ok := p.span(part); ok {
		p.report(2295, span, "Quote "+p.spanText(span)+" inside the pattern so its value is matched literally rather than as a glob.", p.quoteFix(span, Review))
	}
}

func (p *pass) enclosingValueLoop(name, array string, read int) (*syntax.Word, bool) {
	for depth := 0; ; depth++ {
		switch n := p.ancestor(depth).(type) {
		case nil:
			return nil, false
		case *syntax.ForClause:
			iter, ok := n.Loop.(*syntax.WordIter)
			if !ok || iter.Name == nil || iter.Name.Value != name {
				continue
			}
			for _, w := range iter.Items {
				if iterated, ok := allValuesOf(w); ok && iterated == array {
					return w, !p.reassignedIn(n, name, read)
				}
			}
		}
	}
}

func allValuesOf(w *syntax.Word) (string, bool) {
	pe, ok := parser.SolePart(w).(*syntax.ParamExp)
	if !ok || pe.Short || !isIndexedParam(pe) {
		return "", false
	}
	if index, ok := parser.IndexLit(pe.Index); !ok || index != "@" {
		return "", false
	}
	return pe.Param.Value, true
}

func (p *pass) reassignedIn(loop *syntax.ForClause, name string, read int) bool {
	body := parser.Offset(loop.Loop.End())
	scope := p.tree.ScopeAt(body)
	for def := range p.tree.Visible(name, scope).All() {
		if def.Kind != parser.DefLoop && def.NameSpan.Start > body && def.NameSpan.Start < read {
			return true
		}
	}
	return false
}

func checkValueAsKey(p *pass, pe *syntax.ParamExp) {
	if pe.Param == nil || !isIndexedParam(pe) {
		return
	}
	index, ok := pe.Index.(*syntax.Word)
	if !ok || len(index.Parts) != 1 {
		return
	}
	var key string
	var at syntax.Node = pe
	switch part := index.Parts[0].(type) {
	case *syntax.Lit:
		key = part.Value
	case *syntax.ParamExp:
		if !parser.IsPlainParam(part) {
			return
		}
		key = part.Param.Value
		if !pe.Short {
			at = part
		}
	default:
		return
	}
	list, ok := p.enclosingValueLoop(key, pe.Param.Value, parser.Offset(pe.Pos()))
	if !ok {
		return
	}
	p.reportNode(2302, list, "This loop iterates over the values of "+pe.Param.Value+", but the loop variable is used as a key below; iterate over \"${!"+pe.Param.Value+"[@]}\" to get the keys.")
	p.reportNode(2303, at, key+" holds a value of "+pe.Param.Value+", not a key; loop over \"${!"+pe.Param.Value+"[@]}\" if you need the keys.")
}

func checkArithmeticIndex(p *pass, a *syntax.Assign) {
	if a.Name == nil || a.Index == nil {
		return
	}
	w, ok := a.Index.(*syntax.Word)
	if !ok || len(w.Parts) != 1 {
		return
	}
	arith, ok := w.Parts[0].(*syntax.ArithmExp)
	if !ok || arith.Bracket {
		return
	}
	span, ok := p.span(arith)
	if !ok || p.tree.VisibleAttrs(a.Name.Value, p.tree.ScopeAt(span.Start)).Has(parser.AttrAssoc) {
		return
	}
	p.report(2321, span, "Array indices are already arithmetic; drop the $(( )) around this index.",
		safeFix("Remove the $(( ))", textedit.Edit{Start: span.Start, End: span.Start + 3}, textedit.Edit{Start: span.End - 2, End: span.End}))
}

func checkRedundantParentheses(p *pass, n syntax.Node) {
	switch n := n.(type) {
	case *syntax.ParenArithm:
		if inner, ok := n.X.(*syntax.ParenArithm); ok && !p.inExpansionIndex() {
			p.reportParentheses(2322, inner, "These doubled parentheses are redundant; one pair is enough.")
		}
	case *syntax.ArithmExp:
		p.wholeParentheses(n.X, "arithmetic expansion")
	case *syntax.ArithmCmd:
		p.wholeParentheses(n.X, "arithmetic command")
	case *syntax.CStyleLoop:
		p.wholeParentheses(n.Init, "for loop")
		p.wholeParentheses(n.Cond, "for loop")
		p.wholeParentheses(n.Post, "for loop")
	case *syntax.Assign:
		p.wholeParentheses(n.Index, "array index")
	}
}

func (p *pass) inExpansionIndex() bool {
	for depth := 0; ; depth++ {
		switch p.ancestor(depth).(type) {
		case *syntax.ParenArithm, *syntax.BinaryArithm, *syntax.UnaryArithm, *syntax.FuncArithm, *syntax.Word:
			continue
		case *syntax.ParamExp:
			return true
		}
		return false
	}
}

func (p *pass) wholeParentheses(expr syntax.ArithmExpr, context string) {
	if paren, ok := expr.(*syntax.ParenArithm); ok {
		p.reportParentheses(2323, paren, "The parentheses around the whole "+context+" are redundant.")
	}
}

func (p *pass) reportParentheses(code Code, paren *syntax.ParenArithm, message string) {
	span, ok := p.span(paren)
	if !ok {
		return
	}
	p.report(code, span, message, safeFix("Remove the redundant parentheses", textedit.Edit{Start: span.Start, End: span.Start + 1}, textedit.Edit{Start: span.End - 1, End: span.End}))
}
