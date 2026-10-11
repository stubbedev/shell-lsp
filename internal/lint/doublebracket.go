package lint

import (
	"cmp"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func TestExpression(cmd syntax.Command) (syntax.TestExpr, bool) {
	t, ok := wellFormedTest(cmd)
	return t.expr, ok
}

func wellFormedTest(cmd syntax.Command) (testCommand, bool) {
	t, ok := testCommandOf(cmd)
	if !ok || (t.name != "[" && t.name != "test" && t.name != "[[") || t.unterminated || t.malformed() || t.expr == nil {
		return testCommand{}, false
	}
	return t, true
}

func ReadsVerbatim(w *syntax.Word) bool {
	if isPattern(w) {
		return false
	}
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit, *syntax.SglQuoted, *syntax.DblQuoted, *syntax.ArithmExp:
		case *syntax.ParamExp:
			if !parser.IsPlainParam(p) || !spacelessSpecials[p.Param.Value] {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func DoubleBrackets(text string, stmt *syntax.Stmt) ([]textedit.Edit, bool) {
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Assigns) > 0 || slices.ContainsFunc(stmt.Redirs, func(r *syntax.Redirect) bool { return isWithin(call, r) }) {
		return nil, false
	}
	t, ok := wellFormedTest(call)
	if !ok {
		return nil, false
	}
	c := bracketConversion{text: text, args: t.args}
	if !c.expr(t.expr) {
		return nil, false
	}
	open, last := wordSpan(call.Args[0]), wordSpan(call.Args[len(call.Args)-1])
	edits := append([]textedit.Edit{{Start: open.Start, End: open.End, NewText: "[["}}, c.edits...)
	if t.style == singleBracket {
		return append(edits, textedit.Edit{Start: last.Start, End: last.End, NewText: "]]"}), true
	}
	return append(edits, textedit.Edit{Start: last.End, End: last.End, NewText: " ]]"}), true
}

type bracketConversion struct {
	text  string
	args  []*syntax.Word
	edits []textedit.Edit
}

func (c *bracketConversion) expr(e syntax.TestExpr) bool {
	switch e := e.(type) {
	case *syntax.Word:
		return ReadsVerbatim(e) && !readsAsOperator(e)
	case *syntax.UnaryTest:
		if e.Op == syntax.TsNot {
			c.operator(e.OpPos, "!")
			return c.expr(e.X)
		}
		w, ok := e.X.(*syntax.Word)
		return ok && c.spelledOperator(e.OpPos) && ReadsVerbatim(w)
	case *syntax.BinaryTest:
		switch e.Op {
		case syntax.AndTest:
			c.operator(e.OpPos, "&&")
			return c.expr(e.X) && c.expr(e.Y)
		case syntax.OrTest:
			c.operator(e.OpPos, "||")
			return c.expr(e.X) && c.expr(e.Y)
		case syntax.TsReMatch:
			return false
		}
		x, okX := e.X.(*syntax.Word)
		y, okY := e.Y.(*syntax.Word)
		return okX && okY && c.spelledOperator(e.OpPos) && ReadsVerbatim(x) && ReadsVerbatim(y)
	case *syntax.ParenTest:
		c.operator(e.Lparen, "(")
		c.operator(e.Rparen, ")")
		return c.expr(e.X)
	}
	return false
}

func (c *bracketConversion) operator(pos syntax.Pos, spelling string) {
	span := wordSpan(c.argAt(pos))
	if c.text[span.Start:span.End] != spelling {
		c.edits = append(c.edits, textedit.Edit{Start: span.Start, End: span.End, NewText: spelling})
	}
}

func (c *bracketConversion) spelledOperator(pos syntax.Pos) bool {
	spelling, ok := parser.Literal(c.argAt(pos))
	if ok {
		c.operator(pos, spelling)
	}
	return ok
}

func (c *bracketConversion) argAt(pos syntax.Pos) *syntax.Word {
	i, _ := slices.BinarySearchFunc(c.args, pos.Offset(), func(w *syntax.Word, offset uint) int { return cmp.Compare(w.Pos().Offset(), offset) })
	return c.args[i]
}

func readsAsOperator(w *syntax.Word) bool {
	lit, ok := w.Parts[0].(*syntax.Lit)
	return ok && (strings.HasPrefix(lit.Value, "-") || lit.Value == "!")
}

func wordSpan(w *syntax.Word) parser.Span {
	return parser.Span{Start: parser.Offset(w.Pos()), End: parser.Offset(w.End())}
}
