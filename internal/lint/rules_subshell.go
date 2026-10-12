package lint

import (
	"strconv"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var subshellFamily = family{
	codes: map[Code]meta{
		2030: {severity: SeverityInfo, bug: true, summary: "A variable changed in a subshell loses the change when the subshell exits."},
		2031: {severity: SeverityInfo, bug: true, summary: "A variable read here still has its value from before a change made in a subshell."},
	},
	rules: []rule{
		{on: nodes(&syntax.ParamExp{}, &syntax.Word{}), node: checkLostModification},
	},
}

func checkLostModification(p *pass, n syntax.Node) {
	var name string
	switch n := n.(type) {
	case *syntax.ParamExp:
		if n.Param == nil {
			return
		}
		name = n.Param.Value
	case *syntax.Word:
		read, ok := parser.ArithmeticRead(p.parent(), n)
		if !ok {
			return
		}
		name = read
	default:
		return
	}
	if !p.settings.extended || !p.tree.Defines(name) {
		return
	}
	p.withFlow(func(flow *flowResult) {
		lost, ok := flow.lostRead(n)
		if !ok {
			return
		}
		span, ok := p.span(n)
		if !ok {
			return
		}
		line := strconv.Itoa(p.tree.SourceLine(lost.site.Start))
		if lost.kind == subshellScript {
			p.report(2030, lost.site, "This change to "+name+" is made in a line or script that runs in a shell of its own, so nothing after it sees the change.")
			p.report(2031, span, name+" was changed on line "+line+", which runs in a shell of its own, so this still sees the value from before that change.")
			return
		}
		cause := "a " + lost.kind.label()
		p.report(2030, lost.site, "This change to "+name+" happens in a subshell caused by "+cause+", so it is gone once that subshell exits.")
		p.report(2031, span, name+" was changed in a subshell ("+cause+" on line "+line+"), so this still sees the value from before that change.")
	})
}
