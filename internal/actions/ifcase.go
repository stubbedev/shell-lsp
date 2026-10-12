package actions

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/internal/textline"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type caseArm struct {
	patterns []string
	body     parser.Span
}

type comparison struct {
	subject  *syntax.Word
	name     string
	patterns []string
}

func ifToCase(s *site) (Change, bool) {
	root, ok := ifChainAt(s)
	if !ok || !root.FiPos.IsValid() || alternateForm(root) {
		return Change{}, false
	}
	tree := s.Tree
	text := tree.Text()
	start := parser.Offset(root.Position)
	if !textline.BlankBefore(text, start) {
		return Change{}, false
	}
	var arms []caseArm
	var tests []comparison
	for clause := root; clause != nil; clause = clause.Else {
		next := root.FiPos
		if clause.Else != nil {
			next = clause.Else.Position
		}
		if !clause.ThenPos.IsValid() {
			if len(clause.Cond) > 0 {
				return Change{}, false
			}
			body, ok := branchBody(tree, parser.Offset(clause.Position)+len("else"), parser.Offset(next), clause.Then)
			if !ok {
				return Change{}, false
			}
			arms = append(arms, caseArm{patterns: []string{"*"}, body: body})
			break
		}
		cmp, ok := conditionComparison(tree, clause)
		if !ok || (len(tests) > 0 && cmp.name != tests[0].name) || commented(tree, parser.Span{Start: parser.Offset(clause.Position), End: parser.Offset(clause.ThenPos)}) {
			return Change{}, false
		}
		tests = append(tests, cmp)
		body, ok := branchBody(tree, parser.Offset(clause.ThenPos)+len("then"), parser.Offset(next), clause.Then)
		if !ok {
			return Change{}, false
		}
		arms = append(arms, caseArm{patterns: cmp.patterns, body: body})
	}
	if len(tests) < 2 {
		return Change{}, false
	}
	subject, _ := tree.NodeSpan(tests[0].subject)
	verbatim := verbatimLinesOf(tree)
	unit := indentUnit(tree, verbatim, s.Format.Indent)
	indent := leadingSpace(text, textline.Start(text, start))
	armIndent := indent
	if s.Format.SwitchCaseIndent {
		armIndent += unit
	}
	var sb strings.Builder
	sb.WriteString("case " + text[subject.Start:subject.End] + " in\n")
	for _, arm := range arms {
		sb.WriteString(armIndent + strings.Join(arm.patterns, "|") + ")\n")
		sb.WriteString(reindent(tree, verbatim, arm.body, armIndent+unit))
		sb.WriteString(armIndent + unit + ";;\n")
	}
	sb.WriteString(indent + "esac")
	return Change{Title: "Convert to case", Edits: []textedit.Edit{{Start: start, End: parser.Offset(root.FiPos) + len("fi"), NewText: sb.String()}}}, true
}

func alternateForm(root *syntax.IfClause) bool {
	for clause := root; clause != nil; clause = clause.Else {
		if clause.Braces || clause.Short {
			return true
		}
	}
	return false
}

func ifChainAt(s *site) (*syntax.IfClause, bool) {
	path := s.pathAt()
	i := len(path) - 1
	for i >= 0 {
		if _, ok := path[i].Node.(*syntax.IfClause); ok {
			break
		}
		i--
	}
	if i < 0 {
		return nil, false
	}
	root, _ := path[i].Node.(*syntax.IfClause)
	for ; i > 0; i-- {
		parent, ok := path[i-1].Node.(*syntax.IfClause)
		if !ok || parent.Else != root {
			break
		}
		root = parent
	}
	for clause := root; clause != nil && clause.ThenPos.IsValid(); clause = clause.Else {
		if parser.Offset(clause.Position) <= s.sel.Start && s.sel.Start < parser.Offset(clause.ThenPos)+len("then") {
			return root, true
		}
	}
	return nil, false
}

func branchBody(tree *parser.Tree, after, next int, stmts []*syntax.Stmt) (parser.Span, bool) {
	if len(stmts) == 0 {
		return parser.Span{}, false
	}
	text := tree.Text()
	body := parser.Span{Start: after + len(leadingSpace(text, after)), End: next}
	if textline.End(text, after) < next && textline.BlankAfter(text, after) {
		body.Start = textline.End(text, after) + 1
	}
	if textline.BlankBefore(text, next) {
		body.End = textline.Start(text, next)
	} else {
		body.End = statementsEnd(text, body.Start, next, stmts[len(stmts)-1])
	}
	for _, h := range tree.Heredocs {
		if body.Start <= h.Span.Start && h.Span.Start < body.End && heredocBody(text, h).End >= body.End {
			return parser.Span{}, false
		}
	}
	return body, body.Start < body.End
}

func statementsEnd(text string, start, next int, last *syntax.Stmt) int {
	end := start + len(strings.TrimRight(text[start:next], " \t"))
	if last.Semicolon.IsValid() && parser.Offset(last.Semicolon) == end-1 && text[end-1] == ';' {
		end = start + len(strings.TrimRight(text[start:end-1], " \t"))
	}
	return end
}

func commented(tree *parser.Tree, span parser.Span) bool {
	return slices.ContainsFunc(tree.Comments, func(c parser.Comment) bool { return span.Start <= c.Span.Start && c.Span.Start < span.End })
}

func conditionComparison(tree *parser.Tree, clause *syntax.IfClause) (comparison, bool) {
	if len(clause.Cond) != 1 {
		return comparison{}, false
	}
	stmt := clause.Cond[0]
	if stmt.Negated || stmt.Background || stmt.Coprocess || stmt.Disown || len(stmt.Redirs) > 0 {
		return comparison{}, false
	}
	return commandComparison(tree, stmt.Cmd)
}

func commandComparison(tree *parser.Tree, cmd syntax.Command) (comparison, bool) {
	if bin, ok := cmd.(*syntax.BinaryCmd); ok {
		if bin.Op != syntax.OrStmt || bin.X.Negated || bin.Y.Negated || len(bin.X.Redirs)+len(bin.Y.Redirs) > 0 {
			return comparison{}, false
		}
		x, okX := commandComparison(tree, bin.X.Cmd)
		y, okY := commandComparison(tree, bin.Y.Cmd)
		return x.or(y, okX && okY)
	}
	if call, ok := cmd.(*syntax.CallExpr); ok && len(call.Assigns) > 0 {
		return comparison{}, false
	}
	expr, ok := lint.TestExpression(cmd)
	if !ok {
		return comparison{}, false
	}
	_, double := cmd.(*syntax.TestClause)
	return testComparison(tree, expr, double)
}

func testComparison(tree *parser.Tree, expr syntax.TestExpr, double bool) (comparison, bool) {
	switch e := expr.(type) {
	case *syntax.ParenTest:
		return testComparison(tree, e.X, double)
	case *syntax.BinaryTest:
		if e.Op == syntax.OrTest {
			x, okX := testComparison(tree, e.X, double)
			y, okY := testComparison(tree, e.Y, double)
			return x.or(y, okX && okY)
		}
		if e.Op != syntax.TsMatch && e.Op != syntax.TsMatchShort {
			return comparison{}, false
		}
		subject, okX := e.X.(*syntax.Word)
		pattern, okY := e.Y.(*syntax.Word)
		if !okX || !okY {
			return comparison{}, false
		}
		name, ok := subjectName(subject, double)
		if !ok || !casePattern(pattern, double) {
			return comparison{}, false
		}
		span, _ := tree.NodeSpan(pattern)
		return comparison{subject: subject, name: name, patterns: []string{tree.Text()[span.Start:span.End]}}, true
	}
	return comparison{}, false
}

func (c comparison) or(other comparison, ok bool) (comparison, bool) {
	if !ok || c.name != other.name {
		return comparison{}, false
	}
	c.patterns = append(slices.Clip(c.patterns), other.patterns...)
	return c, true
}

func subjectName(w *syntax.Word, double bool) (string, bool) {
	pe, ok := parser.SoleParam(w)
	if !ok || !parser.IsPlainParam(pe) {
		return "", false
	}
	if dq, quoted := w.Parts[0].(*syntax.DblQuoted); quoted && dq.Dollar || !quoted && !double {
		return "", false
	}
	return pe.Param.Value, true
}

func casePattern(w *syntax.Word, double bool) bool {
	if lit, ok := parser.Literal(w); ok && lit == "esac" && len(w.Parts) == 1 {
		return false
	}
	if !double {
		return lint.ReadsVerbatim(w)
	}
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.ExtGlob:
			return false
		case *syntax.Lit:
			if strings.ContainsAny(p.Value, "(|") {
				return false
			}
		}
	}
	return true
}
