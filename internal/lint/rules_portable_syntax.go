package lint

import (
	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/parser"
)

var portableSyntaxFamily = family{
	codes: map[Code]meta{
		3001: {severity: SeverityWarning},
		3005: {severity: SeverityWarning},
		3006: {severity: SeverityWarning},
		3008: {severity: SeverityWarning},
		3024: {severity: SeverityWarning},
		3029: {severity: SeverityWarning},
		3030: {severity: SeverityWarning},
		3032: {severity: SeverityWarning},
		3033: {severity: SeverityWarning},
		3034: {severity: SeverityWarning},
		3035: {severity: SeverityWarning},
	},
	rules: []rule{{on: nodes(&syntax.ProcSubst{}, &syntax.ForClause{}, &syntax.ArithmCmd{}, &syntax.Assign{}, &syntax.BinaryCmd{}, &syntax.ArrayExpr{}, &syntax.CoprocClause{}, &syntax.FuncDecl{}, &syntax.CmdSubst{}), node: checkPortableSyntax}},
}

func checkPortableSyntax(p *pass, n syntax.Node) {
	if !strictShells.Has(p.dialect) {
		return
	}
	switch n := n.(type) {
	case *syntax.ProcSubst:
		p.nonPortableNode(portabilityIssue{code: 3001, shells: strictShells, feature: "Process substitution " + n.Op.String() + "...)", alternative: "a temporary file or a pipe"}, n)
	case *syntax.ForClause:
		switch {
		case n.Select:
			p.nonPortableNode(portabilityIssue{code: 3008, shells: strictShells, feature: "A select loop"}, n)
		case isCStyleLoop(n):
			p.nonPortableNode(portabilityIssue{code: 3005, shells: strictShells, feature: "A C-style for ((...)) loop", alternative: "a while loop with $((...)) arithmetic"}, n)
		}
	case *syntax.ArithmCmd:
		p.nonPortableNode(portabilityIssue{code: 3006, shells: strictShells, feature: "The arithmetic command ((...))", alternative: `[ "$((...))" -ne 0 ] or a $((...)) assignment`}, n)
	case *syntax.Assign:
		if n.Append && n.Name != nil {
			p.nonPortableNode(portabilityIssue{code: 3024, shells: strictShells, feature: "Appending with " + n.Name.Value + "+=", alternative: n.Name.Value + `="$` + n.Name.Value + `..."`}, n)
		}
	case *syntax.BinaryCmd:
		if n.Op == syntax.PipeAll {
			p.nonPortable(portabilityIssue{code: 3029, shells: strictShells, feature: "The |& operator", alternative: "2>&1 |"}, posSpan(n.OpPos, len("|&")))
		}
	case *syntax.ArrayExpr:
		p.nonPortableNode(portabilityIssue{code: 3030, shells: strictShells, feature: "An array"}, n)
	case *syntax.CoprocClause:
		p.nonPortableNode(portabilityIssue{code: 3032, shells: strictShells, feature: "coproc", alternative: "named pipes (mkfifo)"}, n)
	case *syntax.FuncDecl:
		if n.Name != nil && !parser.IsName(n.Name.Value) {
			p.nonPortableNode(portabilityIssue{code: 3033, shells: strictShells, feature: "The function name " + n.Name.Value + ", which is not a valid identifier,"}, n)
		}
	case *syntax.CmdSubst:
		checkFileReadShortcut(p, n)
	}
}

func isCStyleLoop(f *syntax.ForClause) bool {
	_, ok := f.Loop.(*syntax.CStyleLoop)
	return ok
}

func checkFileReadShortcut(p *pass, cs *syntax.CmdSubst) {
	if len(cs.Stmts) != 1 {
		return
	}
	stmt := cs.Stmts[0]
	if stmt.Cmd != nil || len(stmt.Redirs) == 0 {
		return
	}
	if cs.Backquotes {
		p.nonPortableNode(portabilityIssue{code: 3035, shells: strictShells, feature: "Reading a file with `< file`", alternative: "`cat file`"}, cs)
		return
	}
	p.nonPortableNode(portabilityIssue{code: 3034, shells: strictShells, feature: "Reading a file with $(< file)", alternative: "$(cat file)"}, cs)
}
