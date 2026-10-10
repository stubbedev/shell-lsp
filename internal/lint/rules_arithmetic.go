package lint

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

var arithmeticFamily = family{
	codes: map[Code]meta{
		2017: {severity: SeverityInfo},
		2079: {severity: SeverityError},
	},
	rules: []rule{
		{node: checkDivisionBeforeMultiplication},
		{finish: checkDecimalArithmetic},
	},
}

func checkDivisionBeforeMultiplication(p *pass, n syntax.Node) {
	mul, ok := n.(*syntax.BinaryArithm)
	if !ok || mul.Op != syntax.Mul || p.dialect == dialect.Ksh {
		return
	}
	div, ok := mul.X.(*syntax.BinaryArithm)
	if !ok || div.Op != syntax.Quo {
		return
	}
	divisor, okDivisor := p.span(div.Y)
	multiplier, okMultiplier := p.span(mul.Y)
	if !okDivisor || !okMultiplier || p.text[divisor.Start:divisor.End] == p.text[multiplier.Start:multiplier.End] {
		return
	}
	p.report(2017, opAt(div.OpPos, "/").span, "Integer division truncates before the multiplication; multiply first, as in a * 100 / b, to keep precision.")
}

func checkDecimalArithmetic(p *pass) {
	if p.dialect == dialect.Ksh {
		return
	}
	e, ok := p.rootParseError()
	if !ok {
		return
	}
	if number, ok := p.decimalAt(e.Span.Start); ok {
		p.report(2079, number, "Shell arithmetic is integer only, so "+p.text[number.Start:number.End]+" is not a valid number; use bc or awk for decimals.")
	}
}

func (p *pass) decimalAt(dot int) (parser.Span, bool) {
	text := p.text
	if dot >= len(text) || text[dot] != '.' {
		return parser.Span{}, false
	}
	start := dot
	for start > 0 && isDigit(text[start-1]) {
		start--
	}
	if start == dot || (start > 0 && isNameChar(text[start-1])) || p.indexesTextKeys(start) {
		return parser.Span{}, false
	}
	end := dot + 1
	for end < len(text) && isDigit(text[end]) {
		end++
	}
	return parser.Span{Start: start, End: end}, true
}

func (p *pass) indexesTextKeys(number int) bool {
	if number == 0 || p.text[number-1] != '[' {
		return false
	}
	nameEnd := number - 1
	nameStart := nameEnd
	for nameStart > 0 && isNameChar(p.text[nameStart-1]) {
		nameStart--
	}
	if strings.HasSuffix(p.text[:nameStart], "${") {
		return true
	}
	name := p.text[nameStart:nameEnd]
	return parser.IsName(name) && p.tree.VisibleAttrs(name, p.tree.ScopeAt(nameStart)).Has(parser.AttrAssoc)
}
