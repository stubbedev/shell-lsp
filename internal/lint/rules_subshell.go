package lint

import (
	"strconv"

	"mvdan.cc/sh/v3/syntax"
)

var subshellFamily = family{
	codes: map[Code]meta{
		2030: {severity: SeverityInfo},
		2031: {severity: SeverityInfo},
	},
	rules: []rule{
		{on: nodes(&syntax.ParamExp{}, &syntax.Word{}), node: checkLostModification},
	},
}

var subshellCauses = map[subshellKind]string{
	subshellGroup:      "a ( ) group",
	subshellPipeline:   "a pipeline",
	subshellExpansion:  "a command substitution",
	subshellBackground: "running in the background",
	subshellCoproc:     "a coprocess",
	subshellBats:       "a separate Bats test",
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
		name = n.Lit()
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
		cause := subshellCauses[lost.kind]
		p.report(2030, lost.site, "This change to "+name+" happens in a subshell caused by "+cause+", so it is gone once that subshell exits.")
		p.report(2031, span, name+" was changed in a subshell ("+cause+" on line "+strconv.Itoa(p.tree.Line(lost.site.Start))+"), so this still sees the value from before that change.")
	})
}
