package lint

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var exprFamily = family{
	codes: map[Code]meta{
		2003: {severity: SeverityStyle},
		2304: {severity: SeverityError},
		2305: {severity: SeverityWarning},
		2306: {severity: SeverityWarning},
		2307: {severity: SeverityWarning},
		2308: {severity: SeverityInfo},
	},
	commands: []commandRule{
		{match: matchBase, names: []string{"expr"}, check: checkExprOutdated},
		{match: matchBase, names: []string{"expr"}, check: checkExprShape},
	},
}

var (
	exprWithoutReplacement = setOf(":", "<", ">", "<=", ">=", "match", "length", "substr", "index")
	exprStringReplacements = map[string]string{
		"match":  "[[ string =~ regex ]]",
		"length": "${#var}",
		"substr": "${var:offset:length}",
		"index":  "a parameter expansion such as ${var%%[chars]*}",
	}
)

func checkExprOutdated(p *pass, c command) {
	if slices.ContainsFunc(c.args, func(w *syntax.Word) bool { return literalIn(w, exprWithoutReplacement) }) {
		return
	}
	p.reportNode(2003, c.word, "expr is an antiquated way to do arithmetic; use $((...)) instead.", p.arithmeticExpansionFix(c)...)
}

var exprArithmetic = setOf("+", "-", "*", "/", "%")

func (p *pass) arithmeticExpansionFix(c command) []Fix {
	stmt, ok := p.parent().(*syntax.Stmt)
	cs, csOK := p.ancestor(1).(*syntax.CmdSubst)
	if !ok || !csOK || len(cs.Stmts) != 1 || c.call == nil || stmt.Cmd != c.call || c.call.Args[0] != c.word || len(c.call.Assigns) > 0 || len(stmt.Redirs) > 0 || stmt.Negated || len(c.args)%2 == 0 {
		return nil
	}
	terms := make([]string, len(c.args))
	for i, arg := range c.args {
		term, ok := p.exprTerm(arg, i%2 == 1)
		if !ok {
			return nil
		}
		terms[i] = term
	}
	span, ok := p.span(cs)
	if !ok {
		return nil
	}
	return []Fix{reviewFix("Rewrite as $(( ))", textedit.Edit{Start: span.Start, End: span.End, NewText: "$((" + strings.Join(terms, " ") + "))"})}
}

func (p *pass) exprTerm(w *syntax.Word, operator bool) (string, bool) {
	if lit, ok := parser.Literal(w); ok {
		if operator {
			return lit, exprArithmetic[lit]
		}
		digits := strings.TrimPrefix(lit, "-")
		return lit, digits != "" && parser.IsDigits(digits)
	}
	pe, ok := solePart(w).(*syntax.ParamExp)
	if operator || !ok || pe.Param == nil || !parser.IsName(pe.Param.Value) || !parser.IsPlainParam(pe) {
		return "", false
	}
	return "$" + pe.Param.Value, true
}

func checkExprShape(p *pass, c command) {
	switch args := c.args; {
	case len(args) == 3:
		switch {
		case isUnquotedLiteral(args[1], "*"):
			p.reportNode(2304, args[1], "The shell expands this * as a glob; escape it as \\* for expr multiplication.")
		case isUnquotedLiteral(args[1], ":") && isGlob(args[2]):
			p.reportNode(2305, args[2], "Quote this expr regex so the shell does not expand it as a glob.")
		}
	case len(args) == 1 && !canWordSplit(args[0]):
		p.reportNode(2307, args[0], "expr sees a single argument and just prints it; separate operators and operands with spaces, and escape > < | as needed.")
		return
	case len(args) == 2 && !parser.LiteralIs(args[0], "length") && !slices.ContainsFunc(args, canWordSplit):
		p.reportNode(2307, c.call, "expr sees only two arguments, which is not a valid expression; separate operators and operands with spaces, and escape > < | as needed.")
	default:
		for _, arg := range args[1:] {
			if isGlob(arg) {
				p.reportNode(2306, arg, "Escape or quote glob characters in expr arguments so the shell passes them through unchanged.")
			}
		}
	}
	checkExprStringOperator(p, c)
}

func checkExprStringOperator(p *pass, c command) {
	if len(c.args) == 0 {
		return
	}
	keyword, _ := parser.Literal(c.args[0])
	if replacement, ok := exprStringReplacements[keyword]; ok {
		p.reportNode(2308, c.args[0], "expr "+keyword+" is not specified by POSIX; use "+replacement+" instead.")
	}
}

func isUnquotedLiteral(w *syntax.Word, text string) bool {
	if len(w.Parts) != 1 {
		return false
	}
	lit, ok := w.Parts[0].(*syntax.Lit)
	return ok && lit.Value == text
}

func canWordSplit(w *syntax.Word) bool {
	return mayExpandToSeveralWords(w) || slices.ContainsFunc(w.Parts, func(part syntax.WordPart) bool {
		_, ok := part.(*syntax.CmdSubst)
		return ok
	})
}
