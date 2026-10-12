package parser

import (
	"fmt"

	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

func Walk(node syntax.Node, f func(syntax.Node) bool) {
	if !f(node) {
		return
	}
	var trailing *syntax.Comment
	switch n := node.(type) {
	case *syntax.File:
		walkStmts(n.Stmts, f)
		walkComments(n.Last, f)
	case *syntax.Comment, *syntax.Lit, *syntax.SglQuoted:
	case *syntax.Stmt:
		trailing = walkLeadingComments(n.Comments, func(c *syntax.Comment) bool { return !n.End().After(c.Pos()) }, f)
		if n.Cmd != nil {
			Walk(n.Cmd, f)
		}
		for _, r := range n.Redirs {
			Walk(r, f)
		}
	case *syntax.Assign:
		walkLit(n.Name, f)
		walkWord(n.Value, f)
		walkArithm(n.Index, f)
		walkRefElems(n.Elems, f)
		if n.Array != nil {
			Walk(n.Array, f)
		}
	case *syntax.Redirect:
		walkLit(n.N, f)
		walkWord(n.Word, f)
		walkWord(n.Hdoc, f)
	case *syntax.CallExpr:
		for _, a := range n.Assigns {
			Walk(a, f)
		}
		walkWords(n.Args, f)
	case *syntax.Subshell:
		walkStmts(n.Stmts, f)
		walkComments(n.Last, f)
	case *syntax.Block:
		walkStmts(n.Stmts, f)
		walkComments(n.Last, f)
	case *syntax.IfClause:
		walkStmts(n.Cond, f)
		walkComments(n.CondLast, f)
		walkStmts(n.Then, f)
		walkComments(n.ThenLast, f)
		walkComments(n.Last, f)
		if n.Else != nil {
			Walk(n.Else, f)
		}
	case *syntax.WhileClause:
		walkStmts(n.Cond, f)
		walkComments(n.CondLast, f)
		walkStmts(n.Do, f)
		walkComments(n.DoLast, f)
	case *syntax.ForClause:
		Walk(n.Loop, f)
		walkStmts(n.Do, f)
		walkComments(n.DoLast, f)
	case *syntax.TryClause:
		Walk(n.Try, f)
		Walk(n.Always, f)
	case *syntax.NamespaceClause:
		Walk(n.Name, f)
		Walk(n.Body, f)
	case *syntax.RepeatClause:
		Walk(n.Count, f)
		walkStmts(n.Do, f)
		walkComments(n.DoLast, f)
	case *syntax.WordIter:
		walkLit(n.Name, f)
		for _, name := range n.Names {
			Walk(name, f)
		}
		walkWords(n.Items, f)
	case *syntax.CStyleLoop:
		walkArithm(n.Init, f)
		walkArithm(n.Cond, f)
		walkArithm(n.Post, f)
	case *syntax.BinaryCmd:
		Walk(n.X, f)
		Walk(n.Y, f)
	case *syntax.FuncDecl:
		walkLit(n.Name, f)
		for _, name := range n.Names {
			Walk(name, f)
		}
		for _, param := range n.Params {
			Walk(param, f)
		}
		Walk(n.Body, f)
		walkWords(n.Args, f)
	case *syntax.Word:
		walkParts(n.Parts, f)
	case *syntax.DblQuoted:
		walkParts(n.Parts, f)
	case *syntax.CmdSubst:
		walkStmts(n.Stmts, f)
		walkComments(n.Last, f)
	case *syntax.ParamExp:
		walkParamExp(n, f)
	case *syntax.ArithmExp:
		Walk(n.X, f)
	case *syntax.ArithmCmd:
		Walk(n.X, f)
	case *syntax.BinaryArithm:
		Walk(n.X, f)
		Walk(n.Y, f)
	case *syntax.BinaryTest:
		Walk(n.X, f)
		Walk(n.Y, f)
	case *syntax.UnaryArithm:
		Walk(n.X, f)
	case *syntax.UnaryTest:
		Walk(n.X, f)
	case *syntax.ParenArithm:
		Walk(n.X, f)
	case *syntax.FlagsArithm:
		Walk(n.Flags, f)
		if n.X != nil {
			Walk(n.X, f)
		}
	case *syntax.FuncArithm:
		Walk(n.Name, f)
		walkArithm(n.X, f)
	case *syntax.ParenTest:
		Walk(n.X, f)
	case *syntax.CaseClause:
		Walk(n.Word, f)
		for _, item := range n.Items {
			Walk(item, f)
		}
		walkComments(n.Last, f)
	case *syntax.CaseItem:
		trailing = walkLeadingComments(n.Comments, func(c *syntax.Comment) bool { return c.Pos().After(n.Pos()) }, f)
		walkWords(n.Patterns, f)
		walkStmts(n.Stmts, f)
		walkComments(n.Last, f)
	case *syntax.TestClause:
		Walk(n.X, f)
	case *syntax.DeclClause:
		for _, a := range n.Args {
			Walk(a, f)
		}
	case *syntax.ArrayExpr:
		for _, e := range n.Elems {
			Walk(e, f)
		}
		walkStmts(n.Stmts, f)
		walkComments(n.Last, f)
	case *syntax.ArrayElem:
		trailing = walkLeadingComments(n.Comments, func(c *syntax.Comment) bool { return c.Pos().After(n.Pos()) }, f)
		walkArithm(n.Index, f)
		walkWord(n.Value, f)
		if n.Array != nil {
			Walk(n.Array, f)
		}
	case *syntax.ExtGlob:
		Walk(n.Pattern, f)
	case *syntax.ProcSubst:
		walkStmts(n.Stmts, f)
		walkComments(n.Last, f)
	case *syntax.TimeClause:
		if n.Stmt != nil {
			Walk(n.Stmt, f)
		}
	case *syntax.CoprocClause:
		walkWord(n.Name, f)
		Walk(n.Stmt, f)
	case *syntax.LetClause:
		for _, x := range n.Exprs {
			Walk(x, f)
		}
	case *syntax.TestDecl:
		Walk(n.Description, f)
		Walk(n.Body, f)
	default:
		panic(fmt.Sprintf("parser.Walk: unexpected node type %T", node))
	}
	f(nil)
	if trailing != nil {
		Walk(trailing, f)
	}
}

func Any(root syntax.Node, pred func(syntax.Node) bool) bool {
	found := false
	Walk(root, func(n syntax.Node) bool {
		found = found || n != nil && pred(n)
		return !found
	})
	return found
}

func walkLeadingComments(comments []syntax.Comment, trails func(*syntax.Comment) bool, f func(syntax.Node) bool) *syntax.Comment {
	for i := range comments {
		c := &comments[i]
		if trails(c) {
			return c
		}
		Walk(c, f)
	}
	return nil
}

func walkParamExp(n *syntax.ParamExp, f func(syntax.Node) bool) {
	if n.Flags != nil {
		Walk(n.Flags, f)
	}
	walkLit(n.Param, f)
	if n.NestedParam != nil {
		Walk(n.NestedParam, f)
	}
	walkArithm(n.Index, f)
	walkRefElems(n.Elems, f)
	if n.Slice != nil {
		walkArithm(n.Slice.Offset, f)
		walkArithm(n.Slice.Length, f)
	}
	if n.Repl != nil {
		walkWord(n.Repl.Orig, f)
		walkWord(n.Repl.With, f)
	}
	if n.Exp != nil {
		walkWord(n.Exp.Word, f)
	}
}

func walkRefElems(elems []syntax.RefElem, f func(syntax.Node) bool) {
	for _, el := range elems {
		walkArithm(el.Index, f)
		walkLit(el.Member, f)
	}
}

func walkStmts(stmts []*syntax.Stmt, f func(syntax.Node) bool) {
	for _, s := range stmts {
		Walk(s, f)
	}
}

func walkWords(words []*syntax.Word, f func(syntax.Node) bool) {
	for _, w := range words {
		Walk(w, f)
	}
}

func walkParts(parts []syntax.WordPart, f func(syntax.Node) bool) {
	for _, part := range parts {
		Walk(part, f)
	}
}

func walkComments(comments []syntax.Comment, f func(syntax.Node) bool) {
	for i := range comments {
		Walk(&comments[i], f)
	}
}

func walkLit(l *syntax.Lit, f func(syntax.Node) bool) {
	if l != nil {
		Walk(l, f)
	}
}

func walkWord(w *syntax.Word, f func(syntax.Node) bool) {
	if w != nil {
		Walk(w, f)
	}
}

func walkArithm(x syntax.ArithmExpr, f func(syntax.Node) bool) {
	if x != nil {
		Walk(x, f)
	}
}
