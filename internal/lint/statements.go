package lint

import (
	"slices"

	"mvdan.cc/sh/v3/syntax"
)

func stmtLists(n syntax.Node) [][]*syntax.Stmt {
	switch n := n.(type) {
	case *syntax.File:
		return [][]*syntax.Stmt{n.Stmts}
	case *syntax.Block:
		return [][]*syntax.Stmt{n.Stmts}
	case *syntax.Subshell:
		return [][]*syntax.Stmt{n.Stmts}
	case *syntax.IfClause:
		return [][]*syntax.Stmt{n.Cond, n.Then}
	case *syntax.WhileClause:
		return [][]*syntax.Stmt{n.Cond, n.Do}
	case *syntax.ForClause:
		return [][]*syntax.Stmt{n.Do}
	case *syntax.CaseItem:
		return [][]*syntax.Stmt{n.Stmts}
	case *syntax.CmdSubst:
		return [][]*syntax.Stmt{n.Stmts}
	case *syntax.ProcSubst:
		return [][]*syntax.Stmt{n.Stmts}
	}
	return nil
}

func rightmost(stmt *syntax.Stmt) *syntax.Stmt {
	for {
		bin, ok := stmt.Cmd.(*syntax.BinaryCmd)
		if !ok {
			return stmt
		}
		stmt = bin.Y
	}
}

func leftmost(stmt *syntax.Stmt) *syntax.Stmt {
	for {
		bin, ok := stmt.Cmd.(*syntax.BinaryCmd)
		if !ok {
			return stmt
		}
		stmt = bin.X
	}
}

func (p *pass) previousStatement() (*syntax.Stmt, bool) {
	stmt, ok := p.parent().(*syntax.Stmt)
	if !ok {
		return nil, false
	}
	owner := p.ancestor(1)
	if bin, ok := owner.(*syntax.BinaryCmd); ok {
		return rightmost(bin.X), bin.Y == stmt
	}
	for _, list := range stmtLists(owner) {
		if i := slices.Index(list, stmt); i > 0 {
			return rightmost(list[i-1]), true
		}
	}
	return nil, false
}
