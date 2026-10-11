package lint

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var arithmeticStyleFamily = family{
	codes: map[Code]meta{
		2004: {severity: SeverityStyle},
		2007: {severity: SeverityStyle, tag: TagDeprecated},
		2080: {severity: SeverityError},
	},
	rules: []rule{
		{on: nodes(&syntax.ParamExp{}), node: checkArithmeticDollar},
		{on: nodes(&syntax.ArithmExp{}), node: checkBracketArithmetic},
		{on: nodes(&syntax.Word{}), node: checkOctalLiteral},
	},
}

const dollarExemptChars = "/.:#%?*@$-!+=^,"

func checkArithmeticDollar(p *pass, n syntax.Node) {
	pe, ok := n.(*syntax.ParamExp)
	if !ok || !pe.Dollar.IsValid() {
		return
	}
	word, ok := p.parent().(*syntax.Word)
	if !ok || len(word.Parts) != 1 || !isArithmeticOperandOf(p.ancestor(1), word) || !p.inArithmeticContext() {
		return
	}
	span, ok := p.span(pe)
	if !ok {
		return
	}
	text := p.text[span.Start:span.End]
	inner := strings.TrimPrefix(text, "$")
	if !pe.Short {
		inner = strings.TrimSuffix(strings.TrimPrefix(inner, "{"), "}")
	}
	if inner == "" || strings.ContainsAny(inner, dollarExemptChars) || parser.IsDigits(inner[:1]) {
		return
	}
	p.report(2004, span, "Arithmetic already expands variable names, so the $ in "+text+" is unnecessary; write "+inner+" instead.")
}

func (p *pass) inArithmeticContext() bool {
	for depth := 1; ; depth++ {
		switch n := p.ancestor(depth).(type) {
		case nil, *syntax.LetClause, *syntax.CallExpr:
			return false
		case *syntax.ParamExp:
			if n.Dollar.IsValid() {
				return false
			}
		case *syntax.Assign:
			return n.Name == nil || !p.associativeArray(n.Name.Value)
		case *syntax.ArithmCmd, *syntax.ArithmExp, *syntax.CStyleLoop:
			return true
		}
	}
}

func checkBracketArithmetic(p *pass, n syntax.Node) {
	exp, ok := n.(*syntax.ArithmExp)
	if !ok || !exp.Bracket {
		return
	}
	if span, ok := p.span(exp); ok {
		p.report(2007, span, "$[ ] is an obsolete arithmetic form; use $(( )) instead.")
	}
}

func checkOctalLiteral(p *pass, n syntax.Node) {
	word, ok := n.(*syntax.Word)
	if !ok || !parser.IsArithmetic(p.parent()) {
		return
	}
	lit := word.Lit()
	digits := strings.TrimLeft(strings.TrimPrefix(lit, "0"), "01234567")
	if !strings.HasPrefix(lit, "0") || digits == "" || (digits[0] != '8' && digits[0] != '9') {
		return
	}
	if span, ok := p.span(word); ok {
		p.report(2080, span, "A leading 0 makes "+lit+" an octal number, where "+digits[:1]+" is not a valid digit; drop the leading zeros or write 10#"+lit+".")
	}
}
