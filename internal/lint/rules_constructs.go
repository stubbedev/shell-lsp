package lint

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

var constructsFamily = family{
	codes: map[Code]meta{
		2111: {severity: SeverityError},
		2112: {severity: SeverityWarning},
		2113: {severity: SeverityWarning},
		2118: {severity: SeverityError},
		2127: {severity: SeverityError},
	},
	rules: []rule{{node: checkShellConstructs}},
}

func checkShellConstructs(p *pass, n syntax.Node) {
	switch n := n.(type) {
	case *syntax.FuncDecl:
		p.checkFunctionKeyword(n)
	case *syntax.Stmt:
		p.checkKshPipeStderr(n)
	case *syntax.CaseClause:
		p.checkCaseTerminators(n)
	case *syntax.CmdSubst:
		if (n.TempFile || n.ReplyVar) && strictShells.Has(p.dialect) {
			if span, ok := p.span(n); ok {
				p.report(2127, span, "${ cmd; } command substitution needs bash or ksh; use $(cmd) or a bash or ksh shebang.")
			}
		}
	}
}

func (p *pass) checkFunctionKeyword(fn *syntax.FuncDecl) {
	if !fn.RsrvWord {
		return
	}
	span, ok := p.span(fn)
	if !ok {
		return
	}
	switch {
	case p.dialect == dialect.Ksh && fn.Parens:
		p.report(2111, span, "ksh does not accept both the function keyword and (); use either 'function name' or 'name()'.")
	case strictShells.Has(p.dialect) && fn.Parens:
		p.report(2112, span, "The function keyword is not POSIX; drop it and keep 'name()'.")
	case strictShells.Has(p.dialect):
		p.report(2113, span, "The function keyword is not POSIX; write 'name()' instead.")
	}
}

func (p *pass) checkKshPipeStderr(stmt *syntax.Stmt) {
	if p.dialect != dialect.Ksh || !stmt.Coprocess || stmt.Cmd == nil {
		return
	}
	end := parser.Offset(stmt.Cmd.End())
	rest := p.text[end:]
	at := end + len(rest) - len(strings.TrimLeft(rest, " \t"))
	if !strings.HasPrefix(p.text[at:], "|&") {
		return
	}
	line, _, _ := strings.Cut(p.text[at+2:], "\n")
	if next := strings.TrimSpace(line); next == "" || strings.HasPrefix(next, "#") {
		return
	}
	p.report(2118, parser.Span{Start: at, End: at + 2}, "ksh reads |& as starting a coprocess, not as a pipe of stdout and stderr; use 2>&1 |.")
}

func (p *pass) checkCaseTerminators(c *syntax.CaseClause) {
	resume, falls := false, false
	for _, item := range c.Items {
		resume = resume || item.Op == syntax.Resume
		falls = falls || item.Op == syntax.Fallthrough
	}
	span, ok := p.span(c)
	if !ok {
		return
	}
	switch {
	case resume && (strictShells.Has(p.dialect) || p.dialect == dialect.Ksh):
		p.report(2127, span, "The ;;& case terminator needs bash; use a bash shebang or restructure the branches.")
	case falls && strictShells.Has(p.dialect):
		p.report(2127, span, "The ;& case terminator needs bash or ksh; use a bash or ksh shebang or restructure the branches.")
	}
}
