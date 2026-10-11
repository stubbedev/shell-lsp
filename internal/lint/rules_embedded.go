package lint

import (
	"github.com/stubbedev/shell-lsp/internal/embedded"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
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
		for _, e := range program.Result.Errors {
			p.report(embeddedSyntaxCodes[program.Language], program.Span(e.Start, e.End), program.Language.String()+": "+e.Message)
		}
	}
}
