package lint

import (
	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/embedded"
)

var embeddedSyntaxCodes = [...]Code{
	embedded.Awk: nativeBase + 3001,
	embedded.Sed: nativeBase + 3002,
	embedded.Jq:  nativeBase + 3003,
}

var embeddedFamily = family{
	codes: func() map[Code]meta {
		out := make(map[Code]meta, len(embeddedSyntaxCodes))
		for _, code := range embeddedSyntaxCodes {
			out[code] = meta{severity: SeverityError}
		}
		return out
	}(),
	rules: []rule{{on: nodes(&syntax.Stmt{}), node: checkEmbeddedPrograms}},
}

func checkEmbeddedPrograms(p *pass, n syntax.Node) {
	stmt, ok := n.(*syntax.Stmt)
	if !ok {
		return
	}
	for _, program := range embedded.In(p.tree, stmt) {
		if e := program.Result.Error; e != nil {
			p.report(embeddedSyntaxCodes[program.Language], program.Span(e.Start, e.End), program.Language.String()+": "+e.Message)
		}
	}
}
