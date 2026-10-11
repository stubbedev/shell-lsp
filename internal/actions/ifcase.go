package actions

import (
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
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
	if !ok {
		return Change{}, false
	}
	text := s.Tree.Text()
	start := parser.Offset(root.Position)
	if !startsLine(text, start) || !root.FiPos.IsValid() || !startsLine(text, parser.Offset(root.FiPos)) {
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
			body, ok := branchBody(text, clause.Position, len("else"), next)
			if !ok {
				return Change{}, false
			}
			arms = append(arms, caseArm{patterns: []string{"*"}, body: body})
			break
		}
		cmp, ok := conditionComparison(s.Tree, clause)
		if !ok || (len(tests) > 0 && cmp.name != tests[0].name) {
			return Change{}, false
		}
		tests = append(tests, cmp)
		body, ok := branchBody(text, clause.ThenPos, len("then"), next)
		if !ok {
			return Change{}, false
		}
		arms = append(arms, caseArm{patterns: cmp.patterns, body: body})
	}
	if len(tests) < 2 {
		return Change{}, false
	}
	subject, _ := s.Tree.NodeSpan(tests[0].subject)
	indent := leadingSpace(text, lineStart(text, start))
	var sb strings.Builder
	sb.WriteString("case " + text[subject.Start:subject.End] + " in\n")
	for _, arm := range arms {
		body := text[arm.body.Start:arm.body.End]
		sb.WriteString(indent + strings.Join(arm.patterns, "|") + ")\n")
		sb.WriteString(body)
		sb.WriteString(bodyIndent(body, indent) + ";;\n")
	}
	sb.WriteString(indent + "esac")
	return Change{Title: "Convert to case", Edits: []textedit.Edit{{Start: start, End: parser.Offset(root.FiPos) + len("fi"), NewText: sb.String()}}}, true
}

func ifChainAt(s *site) (*syntax.IfClause, bool) {
	path := s.pathAt()
	i := len(path) - 1
	for i >= 0 {
		if _, ok := path[i].(*syntax.IfClause); ok {
			break
		}
		i--
	}
	if i < 0 {
		return nil, false
	}
	root, _ := path[i].(*syntax.IfClause)
	for ; i > 0; i-- {
		parent, ok := path[i-1].(*syntax.IfClause)
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

func branchBody(text string, keyword syntax.Pos, length int, next syntax.Pos) (parser.Span, bool) {
	after := parser.Offset(keyword) + length
	end := lineEnd(text, after)
	nextAt := parser.Offset(next)
	if strings.TrimSpace(text[after:end]) != "" || !startsLine(text, nextAt) {
		return parser.Span{}, false
	}
	body := parser.Span{Start: min(end+1, len(text)), End: lineStart(text, nextAt)}
	return body, body.Start < body.End && strings.TrimSpace(text[body.Start:body.End]) != ""
}

func bodyIndent(body, fallback string) string {
	for line := range strings.SplitSeq(body, "\n") {
		if strings.TrimSpace(line) != "" {
			return leadingSpace(line, 0)
		}
	}
	return fallback + "\t"
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
	parts := w.Parts
	quoted := false
	if len(parts) == 1 {
		if dq, ok := parts[0].(*syntax.DblQuoted); ok && !dq.Dollar {
			parts, quoted = dq.Parts, true
		}
	}
	if len(parts) != 1 || (!quoted && !double) {
		return "", false
	}
	pe, ok := parts[0].(*syntax.ParamExp)
	if !ok || !parser.IsPlainParam(pe) {
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
