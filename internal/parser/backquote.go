package parser

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

func DollarSubstitution(text string, cs *syntax.CmdSubst, ancestors []syntax.Node) (string, bool) {
	if !cs.Backquotes || !cs.Left.IsValid() || !cs.Right.IsValid() {
		return "", false
	}
	left, right := offset(cs.Left), offset(cs.Right)
	if escapedAt(text, left) || hasCommentOrHeredoc(cs) {
		return "", false
	}
	dq := inDoubleQuotes(ancestors)
	var sb strings.Builder
	sb.WriteString("$(")
	if text[left+1] == '(' {
		sb.WriteByte(' ')
	}
	unescape(&sb, text[left+1:right], func(c byte) bool {
		return c == '$' || c == '`' || c == '\\' || (dq && c == '"')
	})
	sb.WriteByte(')')
	return sb.String(), true
}

func escapedAt(text string, at int) bool {
	n := 0
	for at-n > 0 && text[at-n-1] == '\\' {
		n++
	}
	return n%2 == 1
}

func hasCommentOrHeredoc(cs *syntax.CmdSubst) bool {
	found := false
	Walk(cs, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.Comment:
			found = true
		case *syntax.Redirect:
			found = found || IsHeredoc(n.Op)
		}
		return !found
	})
	return found
}

func inDoubleQuotes(ancestors []syntax.Node) bool {
	for _, node := range slices.Backward(ancestors) {
		switch node.(type) {
		case *syntax.DblQuoted:
			return true
		case *syntax.Stmt, *syntax.CmdSubst, *syntax.ProcSubst, *syntax.File:
			return false
		}
	}
	return false
}
