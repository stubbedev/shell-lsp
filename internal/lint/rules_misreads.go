package lint

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var misreadsFamily = family{
	codes: map[Code]meta{
		1007: {severity: SeverityWarning, summary: "A space after = ends the assignment, so the variable is set to empty for the next command only."},
		1008: {severity: SeverityError, summary: "The shebang names an interpreter that is not a shell this linter recognises."},
		1010: {severity: SeverityWarning, summary: "A keyword is read as a plain word because no ; or line break comes before it."},
		1037: {severity: SeverityError, summary: "$ followed by several digits expands only the first digit as a positional parameter."},
		1064: {severity: SeverityError, summary: "A function body is not a compound command."},
	},
	nodes: []nodeRule{
		onNode(checkSpaceAfterEquals),
		onNode(checkKeywordAsWord),
		onNode(checkKeywordDeclared),
		onNode(checkKeywordAfterTest),
		onNode(checkPositionalAboveNine),
		onNode(checkFunctionBody),
	},
	finish: []func(*pass){checkUnknownInterpreter},
}

type closingKeyword struct {
	role  string
	opens func(syntax.Node) bool
}

var closingKeywords = map[string]closingKeyword{
	"do":   {role: "the start of a loop body", opens: isLoop},
	"done": {role: "the end of a loop", opens: isLoop},
	"then": {role: "the start of an if branch", opens: isIf},
	"fi":   {role: "the end of an if", opens: isIf},
	"esac": {role: "the end of a case", opens: isCase},
}

func isIf(n syntax.Node) bool {
	_, ok := n.(*syntax.IfClause)
	return ok
}

func isCase(n syntax.Node) bool {
	_, ok := n.(*syntax.CaseClause)
	return ok
}

var shellPrefixes = []string{"sh", "ash", "dash", "busybox sh", "bash", "bats", "ksh", "oksh", "zsh"}

func checkSpaceAfterEquals(p *pass, as *syntax.Assign) {
	if as.Naked || as.Value != nil || as.Array != nil || as.Name == nil || as.Name.Value == "IFS" {
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

func checkKeywordAsWord(p *pass, lit *syntax.Lit) {
	keyword, ok := closingKeywords[lit.Value]
	if !ok {
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
	p.reportKeywordAsWord(span, lit.Value, keyword)
}

func checkKeywordDeclared(p *pass, as *syntax.Assign) {
	if !as.Naked || as.Name == nil {
		return
	}
	keyword, ok := closingKeywords[as.Name.Value]
	if _, declared := p.parent().(*syntax.DeclClause); !ok || !declared {
		return
	}
	if span, ok := p.span(as.Name); ok {
		p.reportKeywordAsWord(span, as.Name.Value, keyword)
	}
}

func checkKeywordAfterTest(p *pass, tc *syntax.TestClause) {
	if !tc.Right.IsValid() {
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
	if keyword, ok := closingKeywords[p.text[start:end]]; ok {
		p.reportKeywordAsWord(parser.Span{Start: start, End: end}, p.text[start:end], keyword)
	}
}

func (p *pass) reportKeywordAsWord(span parser.Span, word string, keyword closingKeyword) {
	if !slices.ContainsFunc(p.stack, keyword.opens) {
		return
	}
	p.report(1010, span, "Here "+word+" is read as a plain word, not as "+keyword.role+"; put a ; or line break before it, or quote it if it is meant literally.")
}

func checkPositionalAboveNine(p *pass, pe *syntax.ParamExp) {
	if !pe.Short || pe.Param == nil || len(pe.Param.Value) != 1 || !isDigit(pe.Param.Value[0]) {
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

func checkFunctionBody(p *pass, fn *syntax.FuncDecl) {
	if fn.Name == nil || fn.Body == nil || p.dialect == dialect.Zsh || strings.HasSuffix(fn.Name.Value, "{") {
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
	case *syntax.Block, *syntax.Subshell, *syntax.IfClause, *syntax.WhileClause, *syntax.ForClause, *syntax.RepeatClause, *syntax.TryClause,
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
