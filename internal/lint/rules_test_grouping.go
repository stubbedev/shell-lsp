package lint

import (
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const codeUngroupedTestLogic = nativeBase + 2012

var testGroupingFamily = family{
	codes: map[Code]meta{
		codeUngroupedTestLogic: {severity: SeverityStyle, optional: optionalTestGrouping},
	},
	nodes: []nodeRule{onNode(checkUngroupedTestLogic)},
}

func checkUngroupedTestLogic(p *pass, or *syntax.BinaryTest) {
	if or.Op != syntax.OrTest {
		return
	}
	var edits []textedit.Edit
	for _, operand := range []syntax.TestExpr{or.X, or.Y} {
		and, ok := operand.(*syntax.BinaryTest)
		if !ok || and.Op != syntax.AndTest {
			continue
		}
		if span, ok := p.span(and); ok {
			edits = append(edits, textedit.Edit{Start: span.Start, End: span.Start, NewText: "( "}, textedit.Edit{Start: span.End, End: span.End, NewText: " )"})
		}
	}
	if len(edits) == 0 {
		return
	}
	p.report(codeUngroupedTestLogic, opAt(or.OpPos, "||").span, "&& binds tighter than || inside [[ ]], so the && side is one group here; add parentheses around it to make the grouping visible.",
		safeFix("Add parentheses around the && group", edits...))
}
