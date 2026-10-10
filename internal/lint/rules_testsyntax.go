package lint

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/parser"
)

var testSyntaxFamily = family{
	codes: map[Code]meta{
		1076: {severity: SeverityError},
		1080: {severity: SeverityError},
		1100: {severity: SeverityError},
		1108: {severity: SeverityError},
		1139: {severity: SeverityError},
		1140: {severity: SeverityError},
	},
	rules: []rule{
		{node: checkTestCommandSyntax},
	},
}

var (
	testBinaryOperatorsLongestFirst = binaryTestOperatorsLongestFirst()
	testBinaryOperators             = setOf(testBinaryOperatorsLongestFirst...)
	testConnectives                 = setOf("!", "(", ")", `\(`, `\)`, "-a", "-o")
	unicodeDashes                   = "\u058a\u05be\u2010\u2011\u2012\u2013\u2014\u2015\ufe63\uff0d"
	testConjunctions                = []struct{ prefix, replacement string }{{"-o", "||"}, {"-a", "&&"}, {"or", "||"}, {"and", "&&"}}
)

func binaryTestOperatorsLongestFirst() []string {
	out := slices.Clone(testSymbolOperators)
	for op := range binaryTestOperators {
		if op[0] == '-' {
			out = append(out, op)
		}
	}
	slices.SortFunc(out, func(a, b string) int { return cmp.Or(cmp.Compare(len(b), len(a)), strings.Compare(a, b)) })
	return out
}

func checkTestCommandSyntax(p *pass, n syntax.Node) {
	call, ok := n.(*syntax.CallExpr)
	if !ok {
		return
	}
	test, ok := testCommandOf(call)
	if !ok {
		return
	}
	args := test.args
	if closing := closingBracket(args); test.style == singleBracket && closing >= 0 {
		p.checkAfterClosingBracket(args[closing+1:])
		args = args[:closing]
	} else if test.unterminated {
		p.checkUnclosedBracket(call)
	}
	for i, arg := range args {
		p.checkTestArgument(args, i, arg)
	}
}

func closingBracket(args []*syntax.Word) int {
	for i, arg := range args {
		if arg.Lit() == "]" {
			return i
		}
	}
	return -1
}

func (p *pass) checkAfterClosingBracket(extra []*syntax.Word) {
	if len(extra) == 0 {
		return
	}
	span, ok := p.span(extra[0])
	if !ok {
		return
	}
	text := p.text[span.Start:span.End]
	for _, c := range testConjunctions {
		if strings.HasPrefix(text, c.prefix) {
			p.report(1139, parser.Span{Start: span.Start, End: span.Start + len(c.prefix)}, "Join test commands with "+c.replacement+", not "+c.prefix+"; outside [ ] it is just another word.")
			return
		}
	}
	p.report(1140, parser.Span{Start: span.Start, End: span.Start}, "These words follow the closing ] and become arguments of the test; add && or || or a line break before them.")
}

func (p *pass) checkUnclosedBracket(call *syntax.CallExpr) {
	end := parser.Offset(call.End())
	rest := strings.TrimLeft(p.text[end:], " \t")
	if !strings.HasPrefix(rest, "\n") {
		return
	}
	at := len(p.text) - len(rest)
	p.report(1080, parser.Span{Start: at, End: at + 1}, "A [ test cannot continue on the next line without a backslash before the line break; add one or close the test with ] on this line.")
}

func (p *pass) checkTestArgument(args []*syntax.Word, i int, arg *syntax.Word) {
	span, ok := p.span(arg)
	if !ok {
		return
	}
	lit := arg.Lit()
	if startsArithmetic(arg) && i > 0 && isTestOperand(args[i-1]) {
		p.report(1076, parser.Span{Start: span.Start, End: span.Start + 1}, "[ ] does not do arithmetic, so "+p.text[span.Start:span.Start+1]+" is just part of a word here; compute the value with $(( )) instead.")
		return
	}
	if r, size := utf8.DecodeRuneInString(lit); lit != "" && strings.ContainsRune(unicodeDashes, r) && size < len(lit) && isASCIILetter(lit[size]) {
		p.report(1100, parser.Span{Start: span.Start, End: span.Start + size}, "This is a Unicode dash, not a hyphen-minus; retype it as - so the test operator is recognised.")
		return
	}
	operandBefore := i == 0 || !testBinaryOperators[args[i-1].Lit()]
	operandAfter := i+1 < len(args) && !testBinaryOperators[args[i+1].Lit()]
	if operandBefore && operandAfter {
		p.checkGluedOperator(arg)
	}
}

func startsArithmetic(w *syntax.Word) bool {
	lit, ok := w.Parts[0].(*syntax.Lit)
	if !ok || lit.Value == "" {
		return false
	}
	return strings.IndexByte("+*/%", lit.Value[0]) >= 0 || (lit.Value == "-" && len(w.Parts) == 1)
}

func isTestOperand(w *syntax.Word) bool {
	lit := w.Lit()
	if testBinaryOperators[lit] || testConnectives[lit] || strings.HasPrefix(lit, "!-") {
		return false
	}
	return !strings.HasPrefix(lit, "-") || len(lit) == 1
}

func (p *pass) checkGluedOperator(arg *syntax.Word) {
	last, ok := arg.Parts[len(arg.Parts)-1].(*syntax.Lit)
	if !ok || testBinaryOperators[arg.Lit()] || testConnectives[arg.Lit()] {
		return
	}
	for _, op := range testBinaryOperatorsLongestFirst {
		before, found := strings.CutSuffix(last.Value, op)
		if !found {
			continue
		}
		if len(arg.Parts) == 1 && (before == "" || !isNameChar(before[len(before)-1])) {
			return
		}
		if span, ok := p.span(last); ok {
			p.report(1108, span, "Put spaces around "+op+"; glued to its operand it is part of the word, not a comparison.")
		}
		return
	}
}
