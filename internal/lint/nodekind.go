package lint

import "mvdan.cc/sh/v3/syntax"

type nodeKind uint8

const (
	kindUnknown nodeKind = iota
	kindArithmCmd
	kindArithmExp
	kindArrayElem
	kindArrayExpr
	kindAssign
	kindBinaryArithm
	kindBinaryCmd
	kindBinaryTest
	kindBlock
	kindBraceExp
	kindCallExpr
	kindCaseClause
	kindCaseItem
	kindCmdSubst
	kindComment
	kindCoprocClause
	kindCStyleLoop
	kindDblQuoted
	kindDeclClause
	kindExtGlob
	kindFile
	kindFlagsArithm
	kindForClause
	kindFuncDecl
	kindIfClause
	kindLetClause
	kindLit
	kindParamExp
	kindParenArithm
	kindParenTest
	kindProcSubst
	kindRedirect
	kindSglQuoted
	kindStmt
	kindSubshell
	kindTestClause
	kindTestDecl
	kindTimeClause
	kindUnaryArithm
	kindUnaryTest
	kindWhileClause
	kindWord
	kindWordIter
	nodeKinds
)

func nodeKindOf(n syntax.Node) nodeKind {
	switch n.(type) {
	case *syntax.ArithmCmd:
		return kindArithmCmd
	case *syntax.ArithmExp:
		return kindArithmExp
	case *syntax.ArrayElem:
		return kindArrayElem
	case *syntax.ArrayExpr:
		return kindArrayExpr
	case *syntax.Assign:
		return kindAssign
	case *syntax.BinaryArithm:
		return kindBinaryArithm
	case *syntax.BinaryCmd:
		return kindBinaryCmd
	case *syntax.BinaryTest:
		return kindBinaryTest
	case *syntax.Block:
		return kindBlock
	case *syntax.BraceExp:
		return kindBraceExp
	case *syntax.CallExpr:
		return kindCallExpr
	case *syntax.CaseClause:
		return kindCaseClause
	case *syntax.CaseItem:
		return kindCaseItem
	case *syntax.CmdSubst:
		return kindCmdSubst
	case *syntax.Comment:
		return kindComment
	case *syntax.CoprocClause:
		return kindCoprocClause
	case *syntax.CStyleLoop:
		return kindCStyleLoop
	case *syntax.DblQuoted:
		return kindDblQuoted
	case *syntax.DeclClause:
		return kindDeclClause
	case *syntax.ExtGlob:
		return kindExtGlob
	case *syntax.File:
		return kindFile
	case *syntax.FlagsArithm:
		return kindFlagsArithm
	case *syntax.ForClause:
		return kindForClause
	case *syntax.FuncDecl:
		return kindFuncDecl
	case *syntax.IfClause:
		return kindIfClause
	case *syntax.LetClause:
		return kindLetClause
	case *syntax.Lit:
		return kindLit
	case *syntax.ParamExp:
		return kindParamExp
	case *syntax.ParenArithm:
		return kindParenArithm
	case *syntax.ParenTest:
		return kindParenTest
	case *syntax.ProcSubst:
		return kindProcSubst
	case *syntax.Redirect:
		return kindRedirect
	case *syntax.SglQuoted:
		return kindSglQuoted
	case *syntax.Stmt:
		return kindStmt
	case *syntax.Subshell:
		return kindSubshell
	case *syntax.TestClause:
		return kindTestClause
	case *syntax.TestDecl:
		return kindTestDecl
	case *syntax.TimeClause:
		return kindTimeClause
	case *syntax.UnaryArithm:
		return kindUnaryArithm
	case *syntax.UnaryTest:
		return kindUnaryTest
	case *syntax.WhileClause:
		return kindWhileClause
	case *syntax.Word:
		return kindWord
	case *syntax.WordIter:
		return kindWordIter
	}
	return kindUnknown
}
