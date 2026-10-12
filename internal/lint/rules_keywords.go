package lint

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var keywordFamily = family{
	codes: map[Code]meta{
		2023: {severity: SeverityInfo, bug: true, summary: "The time keyword only accepts -p, so other options need /usr/bin/time."},
		2176: {severity: SeverityWarning, summary: "time on a pipeline is undefined in POSIX sh."},
		2177: {severity: SeverityWarning, summary: "time on a compound command is undefined in POSIX sh."},
		2219: {severity: SeverityStyle, summary: "let is used where an arithmetic command needs no quoting."},
	},
	nodes: []nodeRule{
		onNode(checkTimeClause),
		onNode(checkLet),
	},
}

const timeKeyword = "time"

var (
	timeFlagShells = dialect.SetOf(dialect.Bash, dialect.Posix)
	letShells      = dialect.SetOf(dialect.Bash, dialect.Ksh)
)

func checkTimeClause(p *pass, tc *syntax.TimeClause) {
	if tc.Stmt == nil {
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

func checkLet(p *pass, let *syntax.LetClause) {
	if !letShells.Has(p.dialect) {
		return
	}
	p.reportNode(2219, let, "Use an arithmetic command such as (( a = 1 )) instead of let; it needs no quoting.", p.arithmeticCommandFix(let)...)
}

func unquoted(text string) string {
	for _, quote := range []string{`"`, "'"} {
		if len(text) >= 2 && strings.HasPrefix(text, quote) && strings.HasSuffix(text, quote) && !strings.Contains(text[1:len(text)-1], quote) {
			return text[1 : len(text)-1]
		}
	}
	return text
}

func (p *pass) arithmeticCommandFix(let *syntax.LetClause) []Fix {
	span, ok := p.span(let)
	if !ok || strings.ContainsAny(p.spanText(span), "$`\\") {
		return nil
	}
	exprs := make([]string, len(let.Exprs))
	for i, e := range let.Exprs {
		s, ok := p.span(e)
		if !ok {
			return nil
		}
		exprs[i] = unquoted(p.spanText(s))
	}
	return []Fix{safeFix("Rewrite as (( ))", textedit.Edit{Start: span.Start, End: span.End, NewText: "(( " + strings.Join(exprs, ", ") + " ))"})}
}
