package lint

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var getoptsFamily = family{
	codes: map[Code]meta{
		2213: {severity: SeverityWarning, summary: "getopts accepts an option that the case statement has no branch for."},
		2214: {severity: SeverityWarning, summary: "A case branch handles an option that is not in the getopts string."},
		2220: {severity: SeverityWarning, summary: "A case statement handling getopts options has no *) branch for invalid options."},
	},
	commands: []commandRule{
		{match: matchExact, names: []string{"getopts"}, check: checkGetoptsCase},
	},
}

type patternKind uint8

const (
	patternUnknown patternKind = iota
	patternWildcard
	patternLiteral
)

type casePattern struct {
	word  *syntax.Word
	kind  patternKind
	value string
}

func checkGetoptsCase(p *pass, c command) {
	if len(c.args) < 2 {
		return
	}
	optstring, ok := parser.Literal(c.args[0])
	variable, named := parser.Literal(c.args[1])
	if !ok || !named {
		return
	}
	loop := p.enclosingWhile()
	if loop == nil {
		return
	}
	cc := firstCase(loop.Do)
	if cc == nil || !examines(cc.Word, variable) || p.assignsWithin(variable, loop.Do) {
		return
	}
	letters := strings.ReplaceAll(optstring, ":", "")
	patterns := caseItemPatterns(cc)
	complete, wildcard, catchAll := true, false, false
	for _, pat := range patterns {
		switch {
		case pat.kind == patternUnknown:
			complete = false
		case pat.kind == patternWildcard:
			wildcard, catchAll = true, true
		case pat.value == "?":
			catchAll = true
		case pat.value != ":" && (len(pat.value) != 1 || !strings.Contains(letters, pat.value)):
			p.reportNode(2214, pat.word, "This case branch handles "+pat.value+", which is not an option in the getopts string '"+optstring+"'.")
		}
	}
	if !complete {
		return
	}
	if !wildcard {
		reportUnhandledOptions(p, cc, letters, patterns)
	}
	if !catchAll {
		p.reportNode(2220, cc, "Add a *) branch to this case to handle invalid options reported by getopts.")
	}
}

func reportUnhandledOptions(p *pass, cc *syntax.CaseClause, letters string, patterns []casePattern) {
	for _, letter := range letters {
		if !slices.ContainsFunc(patterns, func(pat casePattern) bool { return pat.value == string(letter) }) {
			p.reportNode(2213, cc, "getopts accepts -"+string(letter)+", but this case has no branch for it.")
		}
	}
}

func (p *pass) enclosingWhile() *syntax.WhileClause {
	for _, n := range slices.Backward(p.stack) {
		if loop, ok := n.(*syntax.WhileClause); ok && !loop.Until {
			return loop
		}
	}
	return nil
}

func firstCase(body []*syntax.Stmt) *syntax.CaseClause {
	for _, stmt := range body {
		if cc, ok := stmt.Cmd.(*syntax.CaseClause); ok {
			return cc
		}
	}
	return nil
}

func examines(w *syntax.Word, variable string) bool {
	pe, ok := soleBareExpansion(w)
	return ok && pe.Param.Value == variable
}

func (p *pass) assignsWithin(variable string, body []*syntax.Stmt) bool {
	if len(body) == 0 {
		return false
	}
	start, end := parser.Offset(body[0].Pos()), parser.Offset(body[len(body)-1].End())
	return slices.ContainsFunc(p.tree.Definitions, func(d parser.Definition) bool {
		return d.Name == variable && d.Span.Start >= start && d.Span.Start < end
	})
}

func caseItemPatterns(cc *syntax.CaseClause) []casePattern {
	var out []casePattern
	for _, item := range cc.Items {
		for _, w := range item.Patterns {
			kind, value := classifyPattern(w)
			out = append(out, casePattern{word: w, kind: kind, value: value})
		}
	}
	return out
}

func classifyPattern(w *syntax.Word) (patternKind, string) {
	if isUnquotedLiteral(w, "*") || isUnquotedLiteral(w, "?") {
		return patternWildcard, ""
	}
	if len(w.Parts) == 1 {
		if lit, ok := w.Parts[0].(*syntax.Lit); ok && len(lit.Value) == 3 && lit.Value[0] == '[' && lit.Value[2] == ']' {
			return patternLiteral, lit.Value[1:2]
		}
	}
	if isGlob(w) {
		return patternUnknown, ""
	}
	if lit, ok := parser.Literal(w); ok {
		return patternLiteral, lit
	}
	return patternUnknown, ""
}
