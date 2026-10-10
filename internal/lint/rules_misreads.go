package lint

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

var misreadsFamily = family{
	codes: map[Code]meta{
		1007: {severity: SeverityWarning},
		1008: {severity: SeverityError},
		1010: {severity: SeverityWarning},
		1037: {severity: SeverityError},
		1064: {severity: SeverityError},
	},
	rules: []rule{
		{node: checkSpaceAfterEquals},
		{node: checkKeywordAsWord},
		{node: checkKeywordDeclared},
		{node: checkKeywordAfterTest},
		{node: checkPositionalAboveNine},
		{node: checkFunctionBody},
		{finish: checkUnknownInterpreter},
	},
}

var closingKeywords = map[string]string{
	"do":   "the start of a loop body",
	"done": "the end of a loop",
	"then": "the start of an if branch",
	"fi":   "the end of an if",
	"esac": "the end of a case",
}

var shellPrefixes = []string{"sh", "ash", "dash", "busybox sh", "bash", "bats", "ksh", "oksh", "zsh"}

func checkSpaceAfterEquals(p *pass, n syntax.Node) {
	as, ok := n.(*syntax.Assign)
	if !ok || as.Naked || as.Value != nil || as.Array != nil || as.Name == nil || as.Name.Value == "IFS" {
		return
	}
	switch p.parent().(type) {
	case *syntax.CallExpr, *syntax.DeclClause:
	default:
		return
	}
	target := as.Name.End()
	if as.Index != nil {
		target = as.Index.End()
	}
	equals := strings.IndexByte(p.text[parser.Offset(target):], '=')
	if equals < 0 {
		return
	}
	start := parser.Offset(target) + equals + 1
	end := start
	for end < len(p.text) {
		switch {
		case p.text[end] == ' ' || p.text[end] == '\t':
			end++
			continue
		case strings.HasPrefix(p.text[end:], "\\\n"):
			end += 2
			continue
		}
		break
	}
	if end == start || end == len(p.text) || strings.IndexByte("\n\r;&|)#", p.text[end]) >= 0 {
		return
	}
	name := p.text[parser.Offset(as.Pos()):start]
	p.report(1007, parser.Span{Start: start, End: end}, "The space after "+name+" ends the assignment, so "+as.Name.Value+" is set to empty only for what follows; remove the space to assign the value, or write "+name+"'' if empty is intended.")
}

func checkKeywordAsWord(p *pass, n syntax.Node) {
	lit, ok := n.(*syntax.Lit)
	if !ok {
		return
	}
	role, keyword := closingKeywords[lit.Value]
	if !keyword {
		return
	}
	word, ok := p.enclosingShellWord()
	if !ok || len(word.Parts) != 1 {
		return
	}
	switch p.ancestor(1).(type) {
	case *syntax.CaseItem:
		if lit.Value != "esac" {
			return
		}
	case *syntax.WordIter:
		if lit.Value == "do" {
			return
		}
	}
	span, ok := p.span(lit)
	if !ok || p.text[span.Start:span.End] != lit.Value {
		return
	}
	p.reportKeywordAsWord(span, lit.Value, role)
}

func checkKeywordDeclared(p *pass, n syntax.Node) {
	as, ok := n.(*syntax.Assign)
	if !ok || !as.Naked || as.Name == nil {
		return
	}
	role, keyword := closingKeywords[as.Name.Value]
	if _, declared := p.parent().(*syntax.DeclClause); !keyword || !declared {
		return
	}
	if span, ok := p.span(as.Name); ok {
		p.reportKeywordAsWord(span, as.Name.Value, role)
	}
}

func checkKeywordAfterTest(p *pass, n syntax.Node) {
	tc, ok := n.(*syntax.TestClause)
	if !ok || !tc.Right.IsValid() {
		return
	}
	start := parser.Offset(tc.Right) + len("]]")
	for start < len(p.text) && (p.text[start] == ' ' || p.text[start] == '\t') {
		start++
	}
	end := start
	for end < len(p.text) && 'a' <= p.text[end] && p.text[end] <= 'z' {
		end++
	}
	if role, keyword := closingKeywords[p.text[start:end]]; keyword {
		p.reportKeywordAsWord(parser.Span{Start: start, End: end}, p.text[start:end], role)
	}
}

func (p *pass) reportKeywordAsWord(span parser.Span, keyword, role string) {
	p.report(1010, span, "Here "+keyword+" is read as a plain word, not as "+role+"; put a ; or line break before it, or quote it if it is meant literally.")
}

func checkPositionalAboveNine(p *pass, n syntax.Node) {
	pe, ok := n.(*syntax.ParamExp)
	if !ok || !pe.Short || pe.Param == nil || len(pe.Param.Value) != 1 || !isDigit(pe.Param.Value[0]) {
		return
	}
	start, end := parser.Offset(pe.Pos()), parser.Offset(pe.End())
	digits := end
	for digits < len(p.text) && isDigit(p.text[digits]) {
		digits++
	}
	if digits == end {
		return
	}
	number := p.text[start+1 : digits]
	p.report(1037, parser.Span{Start: start, End: digits}, "$"+number+" is $"+pe.Param.Value+" followed by the literal "+p.text[end:digits]+"; write ${"+number+"} for positional parameter "+number+".")
}

func isDigit(c byte) bool { return '0' <= c && c <= '9' }

func checkFunctionBody(p *pass, n syntax.Node) {
	fn, ok := n.(*syntax.FuncDecl)
	if !ok || fn.Name == nil || fn.Body == nil || p.dialect == dialect.Zsh || strings.HasSuffix(fn.Name.Value, "{") {
		return
	}
	body := fn.Body
	for {
		list, ok := body.Cmd.(*syntax.BinaryCmd)
		if !ok {
			break
		}
		body = list.X
	}
	switch body.Cmd.(type) {
	case *syntax.Block, *syntax.Subshell, *syntax.IfClause, *syntax.WhileClause, *syntax.ForClause,
		*syntax.CaseClause, *syntax.TestClause, *syntax.ArithmCmd:
		return
	}
	span, ok := p.span(body)
	if !ok {
		return
	}
	p.report(1064, parser.Span{Start: span.Start, End: span.Start}, "The body of "+fn.Name.Value+" must be a compound command; wrap it in { ...; }.")
}

func checkUnknownInterpreter(p *pass) {
	if p.shellChosen {
		return
	}
	name, ok := dialect.Interpreter(p.text)
	if !ok {
		return
	}
	if _, known := dialect.FromName(name); known {
		return
	}
	for _, prefix := range shellPrefixes {
		if strings.HasPrefix(name, prefix) {
			return
		}
	}
	first, _, _ := strings.Cut(p.text, "\n")
	p.report(1008, parser.Span{Start: 0, End: len(first)}, "The shebang names "+name+", which is not a shell this linter recognises; add a directive such as # shellcheck shell=bash to choose the dialect.")
}
