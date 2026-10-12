package lint

import (
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var portableSyntaxFamily = family{
	codes: map[Code]meta{
		3001: {shells: strictShells, summary: "Process substitution is undefined in POSIX sh."},
		3005: {shells: strictShells, summary: "C-style for ((...)) loops are undefined in POSIX sh."},
		3006: {shells: strictShells, summary: "The arithmetic command ((...)) is undefined in POSIX sh."},
		3008: {shells: strictShells, summary: "select loops are undefined in POSIX sh."},
		3024: {shells: strictShells, summary: "Appending with += is undefined in POSIX sh."},
		3029: {shells: strictShells, summary: "The |& operator is undefined in POSIX sh."},
		3030: {shells: strictShells, summary: "Arrays are undefined in POSIX sh."},
		3032: {shells: strictShells, summary: "coproc is undefined in POSIX sh."},
		3033: {shells: strictShells, summary: "A function name that is not a valid identifier is undefined in POSIX sh."},
		3034: {shells: strictShells, summary: "Reading a file with $(< file) is undefined in POSIX sh."},
		3035: {shells: strictShells, summary: "Reading a file with `< file` is undefined in POSIX sh."},
	},
	nodes: []nodeRule{onNodes(checkPortableSyntax, &syntax.ProcSubst{}, &syntax.ForClause{}, &syntax.ArithmCmd{}, &syntax.Assign{}, &syntax.BinaryCmd{}, &syntax.ArrayExpr{}, &syntax.CoprocClause{}, &syntax.FuncDecl{}, &syntax.CmdSubst{})},
}

func checkPortableSyntax(p *pass, n syntax.Node) {
	if !strictShells.Has(p.dialect) {
		return
	}
	switch n := n.(type) {
	case *syntax.ProcSubst:
		p.nonPortableNode(portabilityIssue{code: 3001, feature: "Process substitution " + n.Op.String() + "...)", alternative: "a temporary file or a pipe"}, n)
	case *syntax.ForClause:
		switch {
		case n.Select:
			p.nonPortableNode(portabilityIssue{code: 3008, feature: "A select loop"}, n)
		case isCStyleLoop(n):
			p.nonPortableNode(portabilityIssue{code: 3005, feature: "A C-style for ((...)) loop", alternative: "a while loop with $((...)) arithmetic"}, n)
		}
	case *syntax.ArithmCmd:
		p.nonPortableNode(portabilityIssue{code: 3006, feature: "The arithmetic command ((...))", alternative: `[ "$((...))" -ne 0 ] or a $((...)) assignment`}, n)
	case *syntax.Assign:
		if n.Append && n.Name != nil {
			p.nonPortableNode(portabilityIssue{code: 3024, feature: "Appending with " + n.Name.Value + "+=", alternative: n.Name.Value + `="$` + n.Name.Value + `..."`}, n)
		}
	case *syntax.BinaryCmd:
		if n.Op == syntax.PipeAll {
			p.nonPortable(portabilityIssue{code: 3029, feature: "The |& operator", alternative: "2>&1 |"}, posSpan(n.OpPos, len("|&")))
		}
	case *syntax.ArrayExpr:
		p.nonPortableNode(portabilityIssue{code: 3030, feature: "An array"}, n)
	case *syntax.CoprocClause:
		p.nonPortableNode(portabilityIssue{code: 3032, feature: "coproc", alternative: "named pipes (mkfifo)"}, n)
	case *syntax.FuncDecl:
		if n.Name != nil && !parser.IsName(n.Name.Value) {
			p.nonPortableNode(portabilityIssue{code: 3033, feature: "The function name " + n.Name.Value + ", which is not a valid identifier,"}, n)
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
	var fixes []Fix
	if r := stmt.Redirs[0]; len(stmt.Redirs) == 1 && r.Op == syntax.RdrIn && r.N == nil {
		if file, ok := p.span(r.Word); ok {
			fixes = append(fixes, safeFix("Read the file with cat", textedit.Edit{Start: parser.Offset(r.OpPos), End: file.Start, NewText: "cat "}))
		}
	}
	if cs.Backquotes {
		p.nonPortableNode(portabilityIssue{code: 3035, feature: "Reading a file with `< file`", alternative: "`cat file`"}, cs, fixes...)
		return
	}
	p.nonPortableNode(portabilityIssue{code: 3034, feature: "Reading a file with $(< file)", alternative: "$(cat file)"}, cs, fixes...)
}
