package lint

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

var keywordFamily = family{
	codes: map[Code]meta{
		2023: {severity: SeverityInfo},
		2176: {severity: SeverityWarning},
		2177: {severity: SeverityWarning},
		2219: {severity: SeverityStyle},
	},
	rules: []rule{
		{node: checkTimeClause},
		{node: checkLet},
	},
}

const timeKeyword = "time"

func checkTimeClause(p *pass, n syntax.Node) {
	tc, ok := n.(*syntax.TimeClause)
	if !ok || tc.Stmt == nil {
		return
	}
	at := parser.Offset(tc.Time)
	keyword := parser.Span{Start: at, End: at + len(timeKeyword)}
	switch p.dialect {
	case dialect.Bash, dialect.Posix:
		if call, ok := tc.Stmt.Cmd.(*syntax.CallExpr); ok && !tc.PosixFormat && len(call.Args) > 0 {
			if first, _ := parser.Text(call.Args[0], "_"); strings.HasPrefix(first, "-") {
				p.report(2023, keyword, "The shell keyword time only accepts -p; run /usr/bin/time explicitly to use "+first+".")
			}
		}
	}
	if p.dialect != dialect.Posix && p.dialect != dialect.Dash {
		return
	}
	stages := pipelineStages(tc.Stmt, nil)
	if len(stages) > 1 {
		p.report(2176, keyword, "time on a pipeline is undefined in POSIX sh; time a single simple command or use bash.")
	}
	if _, simple := stages[0].Cmd.(*syntax.CallExpr); !simple {
		p.reportNode(2177, tc.Stmt, "time on a compound command is undefined in POSIX sh; time a single simple command or use bash.")
	}
}

func checkLet(p *pass, n syntax.Node) {
	if _, ok := n.(*syntax.LetClause); !ok || (p.dialect != dialect.Bash && p.dialect != dialect.Ksh) {
		return
	}
	p.reportNode(2219, n, "Use an arithmetic command such as (( a = 1 )) instead of let; it needs no quoting.")
}
