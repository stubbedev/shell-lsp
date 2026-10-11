package lint

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var keywordFamily = family{
	codes: map[Code]meta{
		2023: {severity: SeverityInfo},
		2176: {severity: SeverityWarning},
		2177: {severity: SeverityWarning},
		2219: {severity: SeverityStyle},
	},
	rules: []rule{
		{on: nodes(&syntax.TimeClause{}), node: checkTimeClause},
		{on: nodes(&syntax.LetClause{}), node: checkLet},
	},
}

const timeKeyword = "time"

var (
	timeFlagShells = dialect.SetOf(dialect.Bash, dialect.Posix)
	letShells      = dialect.SetOf(dialect.Bash, dialect.Ksh)
)

func checkTimeClause(p *pass, n syntax.Node) {
	tc, ok := n.(*syntax.TimeClause)
	if !ok || tc.Stmt == nil {
		return
	}
	at := parser.Offset(tc.Time)
	keyword := parser.Span{Start: at, End: at + len(timeKeyword)}
	if timeFlagShells.Has(p.dialect) && !tc.PosixFormat {
		checkTimeFlag(p, tc, keyword)
	}
	if !strictShells.Has(p.dialect) {
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

func checkTimeFlag(p *pass, tc *syntax.TimeClause, keyword parser.Span) {
	call, ok := tc.Stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Args) == 0 {
		return
	}
	if first, _ := parser.Text(call.Args[0], "_"); strings.HasPrefix(first, "-") {
		p.report(2023, keyword, "The shell keyword time only accepts -p; run /usr/bin/time explicitly to use "+first+".")
	}
}

func checkLet(p *pass, n syntax.Node) {
	if _, ok := n.(*syntax.LetClause); !ok || !letShells.Has(p.dialect) {
		return
	}
	p.reportNode(2219, n, "Use an arithmetic command such as (( a = 1 )) instead of let; it needs no quoting.")
}
