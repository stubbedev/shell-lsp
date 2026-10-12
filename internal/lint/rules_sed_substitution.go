package lint

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var sedSubstitutionFamily = family{
	codes: map[Code]meta{
		2001: {severity: SeverityStyle, summary: "sed runs on a single value that a ${var//pattern/replacement} expansion could handle."},
	},
	nodes: []nodeRule{onNodes(checkSedSubstitution, &syntax.BinaryCmd{}, &syntax.Stmt{})},
}

var parameterSubstitutionShells = dialect.SetOf(dialect.Bash, dialect.Ksh)

const sedSubstitutionMessage = "This runs sed on a single value; use the ${variable//pattern/replacement} expansion instead."

func checkSedSubstitution(p *pass, n syntax.Node) {
	if !parameterSubstitutionShells.Has(p.dialect) {
		return
	}
	switch n := n.(type) {
	case *syntax.BinaryCmd:
		if p.isTwoStageEchoSed(n) {
			if span, ok := p.span(n); ok {
				p.report(2001, span, sedSubstitutionMessage)
			}
		}
	case *syntax.Stmt:
		call, ok := n.Cmd.(*syntax.CallExpr)
		if ok && hasHereString(n) && isSimpleSedSubstitution(call) {
			if span, ok := p.span(n); ok {
				p.report(2001, span, sedSubstitutionMessage)
			}
		}
	}
}

func (p *pass) isTwoStageEchoSed(bin *syntax.BinaryCmd) bool {
	if !pipes(bin.Op) {
		return false
	}
	if outer, ok := p.ancestor(1).(*syntax.BinaryCmd); ok && pipes(outer.Op) {
		return false
	}
	echo, ok := bin.X.Cmd.(*syntax.CallExpr)
	if !ok || !isEchoOfOneExpansion(echo) {
		return false
	}
	sed, ok := bin.Y.Cmd.(*syntax.CallExpr)
	return ok && isSimpleSedSubstitution(sed)
}

func isEchoOfOneExpansion(call *syntax.CallExpr) bool {
	if len(call.Args) != 2 {
		return false
	}
	if name, _ := parser.CallName(call); name != "echo" {
		return false
	}
	parts := call.Args[1].Parts
	if len(parts) == 1 {
		if dq, ok := parts[0].(*syntax.DblQuoted); ok {
			parts = dq.Parts
		}
	}
	return len(parts) == 1 && isExpansionPart(parts[0])
}

func isExpansionPart(part syntax.WordPart) bool {
	switch part.(type) {
	case *syntax.ParamExp, *syntax.CmdSubst, *syntax.ArithmExp:
		return true
	}
	return false
}

func isSimpleSedSubstitution(call *syntax.CallExpr) bool {
	if len(call.Args) != 2 && len(call.Args) != 3 {
		return false
	}
	if name, _ := parser.CallName(call); name != "sed" {
		return false
	}
	if flag, _ := parser.Literal(call.Args[1]); len(call.Args) == 3 && flag != "-e" {
		return false
	}
	return isSimpleSubstitute(parser.LiteralChars(call.Args[len(call.Args)-1]))
}

func isSimpleSubstitute(expr string) bool {
	if len(expr) < 2 || expr[0] != 's' || strings.ContainsRune(expr, '\n') {
		return false
	}
	return strings.Count(expr[2:], expr[1:2]) == 2
}

func hasHereString(stmt *syntax.Stmt) bool {
	for _, r := range stmt.Redirs {
		if r.Op == syntax.WordHdoc {
			return true
		}
	}
	return false
}
