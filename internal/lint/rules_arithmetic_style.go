package lint

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var arithmeticStyleFamily = family{
	codes: map[Code]meta{
		2004: {severity: SeverityStyle, summary: "The $ on a variable inside arithmetic is unnecessary."},
		2007: {severity: SeverityStyle, tag: TagDeprecated, summary: "$[ ] is an obsolete arithmetic form."},
		2080: {severity: SeverityError, summary: "A leading 0 makes a number octal, where 8 and 9 are not valid digits."},
	},
	nodes: []nodeRule{
		onNode(checkArithmeticDollar),
		onNode(checkBracketArithmetic),
		onNode(checkOctalLiteral),
	},
}

const dollarExemptChars = "/.:#%?*@$-!+=^,"

func checkArithmeticDollar(p *pass, pe *syntax.ParamExp) {
	if !pe.Dollar.IsValid() {
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
	text := p.spanText(span)
	inner := strings.TrimPrefix(text, "$")
	if !pe.Short {
		inner = strings.TrimSuffix(strings.TrimPrefix(inner, "{"), "}")
	}
	if inner == "" || strings.ContainsAny(inner, dollarExemptChars) || parser.IsDigits(inner[:1]) {
		return
	}
	p.report(2004, span, "Arithmetic already expands variable names, so the $ in "+text+" is unnecessary; write "+inner+" instead.",
		reviewFix("Write "+inner, textedit.Edit{Start: span.Start, End: span.End, NewText: inner}))
}

func (p *pass) inArithmeticContext() bool {
	for depth := 1; ; depth++ {
		switch n := p.ancestor(depth).(type) {
		case nil, *syntax.LetClause, *syntax.CallExpr:
			return false
		case *syntax.ParamExp:
			if n.Dollar.IsValid() || n.Param != nil && n.Index != nil && p.associativeArray(n.Param.Value) {
				return false
			}
		case *syntax.Assign:
			return n.Name == nil || !p.associativeArray(n.Name.Value)
		case *syntax.ArithmCmd, *syntax.ArithmExp, *syntax.CStyleLoop:
			return true
		}
	}
}

func checkBracketArithmetic(p *pass, exp *syntax.ArithmExp) {
	if !exp.Bracket {
		return
	}
	span, ok := p.span(exp)
	if !ok {
		return
	}
	var fixes []Fix
	if strings.HasPrefix(p.text[span.Start:], "$[") && strings.HasSuffix(p.text[:span.End], "]") {
		fixes = append(fixes, safeFix("Rewrite as $(( ))", textedit.Edit{Start: span.Start, End: span.Start + len("$["), NewText: "$(("}, textedit.Edit{Start: span.End - 1, End: span.End, NewText: "))"}))
	}
	p.report(2007, span, "$[ ] is an obsolete arithmetic form; use $(( )) instead.", fixes...)
}

func checkOctalLiteral(p *pass, word *syntax.Word) {
	if !parser.IsArithmetic(p.parent()) {
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
