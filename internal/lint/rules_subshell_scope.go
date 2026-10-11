package lint

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/getopt"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const (
	codeSubshellTrap = nativeBase + 2004
)

var subshellScopeFamily = family{
	codes: map[Code]meta{
		codeSubshellTrap: {severity: SeverityWarning},
	},
	commands: []commandRule{
		{match: matchExact, names: []string{"trap"}, check: checkSubshellTrap},
	},
}

var (
	implicitSubshells = setOf("command substitution", "backtick expansion", "pipeline")
	trapQueries       = setOf("-p", "-l", "-P")
)

func checkSubshellTrap(p *pass, c command) {
	handler, signals, ok := trapOperands(c.args)
	if !ok || len(signals) == 0 {
		return
	}
	if lit, literal := parser.Literal(handler); literal && lit == "-" {
		return
	}
	kind, _ := p.enclosingSubshell()
	if !implicitSubshells[kind] {
		return
	}
	p.reportNode(codeSubshellTrap, c.word, "This trap is set inside a "+kind+", which runs in a subshell, so the handler only applies until that subshell ends and never to the rest of the script.")
}

func trapOperands(args []*syntax.Word) (*syntax.Word, []*syntax.Word, bool) {
	for i, arg := range args {
		lit, literal := parser.Literal(arg)
		switch {
		case literal && lit == getopt.EndOfOptions:
			if i+1 >= len(args) {
				return nil, nil, false
			}
			return args[i+1], args[i+2:], true
		case literal && trapQueries[lit]:
			return nil, nil, false
		case literal && strings.HasPrefix(lit, "-") && len(lit) > 1:
			return nil, nil, false
		default:
			return arg, args[i+1:], true
		}
	}
	return nil, nil, false
}

func (p *pass) enclosingSubshell() (string, int) {
	for depth := 0; p.ancestor(depth) != nil; depth++ {
		n := p.ancestor(depth)
		if _, function := n.(*syntax.FuncDecl); function {
			return "", depth
		}
		if kind := p.subshellKind(n, depth); kind != "" {
			return kind, depth
		}
	}
	return "", -1
}
