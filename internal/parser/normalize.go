package parser

import "github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"

func liftFunctionList(stmt *syntax.Stmt) {
	decl, ok := stmt.Cmd.(*syntax.FuncDecl)
	if !ok || decl.Body == nil || len(decl.Body.Redirs) > 0 {
		return
	}
	list, ok := decl.Body.Cmd.(*syntax.BinaryCmd)
	if !ok {
		return
	}
	head := list
	for {
		inner, ok := head.X.Cmd.(*syntax.BinaryCmd)
		if !ok {
			break
		}
		head = inner
	}
	switch head.X.Cmd.(type) {
	case *syntax.Block, *syntax.Subshell:
	default:
		return
	}
	decl.Body = head.X
	head.X = &syntax.Stmt{Position: stmt.Position, Cmd: decl}
	stmt.Cmd = list
}
