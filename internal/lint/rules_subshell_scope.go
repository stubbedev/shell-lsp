package lint

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/getopt"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const (
	codeSubshellTrap = nativeBase + 2004
	codeSubshellExit = nativeBase + 2005
)

var subshellScopeFamily = family{
	codes: map[Code]meta{
		codeSubshellTrap: {severity: SeverityWarning},
		codeSubshellExit: {severity: SeverityWarning},
	},
	commands: []commandRule{
		{match: matchExact, names: []string{"trap"}, check: checkSubshellTrap},
		{match: matchExact, names: []string{"exit"}, check: checkSubshellExit},
	},
}

var (
	implicitSubshells = setOf("command substitution", "backtick expansion", "pipeline")
	trapQueries       = setOf("-p", "-l", "-P")
	statusParameters  = setOf("?", "PIPESTATUS", "pipestatus")
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

func checkSubshellExit(p *pass, c command) {
	kind, depth := p.enclosingSubshell()
	if !implicitSubshells[kind] {
		return
	}
	var lost bool
	switch n := p.ancestor(depth).(type) {
	case *syntax.CmdSubst:
		lost = p.substitutionExitLost(depth)
	case *syntax.Stmt:
		lost = p.pipelineExitLost(n, depth)
	}
	if lost {
		p.reportNode(codeSubshellExit, c.word, "exit only leaves the subshell this "+kind+" runs in, and nothing checks the status it leaves behind, so the script keeps running; check the status where the "+kind+" ends or avoid the subshell.")
	}
}

func (p *pass) substitutionExitLost(depth int) bool {
	assigned := false
	for d := depth + 1; p.ancestor(d) != nil; d++ {
		switch n := p.ancestor(d).(type) {
		case *syntax.Word, *syntax.DblQuoted, *syntax.ParamExp:
		case *syntax.Assign:
			assigned = true
		case *syntax.CallExpr:
			if !assigned || len(n.Args) > 0 {
				return true
			}
		case *syntax.Stmt:
			return !p.statusChecked(n, d)
		default:
			return true
		}
	}
	return true
}

func (p *pass) pipelineExitLost(stage *syntax.Stmt, depth int) bool {
	outer := depth
	for {
		bin, ok := p.ancestor(outer + 1).(*syntax.BinaryCmd)
		if !ok || !pipes(bin.Op) {
			break
		}
		if _, ok := p.ancestor(outer + 2).(*syntax.Stmt); !ok {
			break
		}
		outer += 2
	}
	pipeline, ok := p.ancestor(outer).(*syntax.Stmt)
	if !ok {
		return false
	}
	stages := pipelineStages(pipeline, nil)
	if stages[len(stages)-1] != stage && !p.options().pipefail {
		return true
	}
	return !p.statusChecked(pipeline, outer)
}

func (p *pass) statusChecked(stmt *syntax.Stmt, depth int) bool {
	if stmt.Negated || p.options().errexit {
		return true
	}
	owner := p.ancestor(depth + 1)
	switch owner := owner.(type) {
	case *syntax.BinaryCmd:
		return true
	case *syntax.IfClause:
		if slices.Contains(owner.Cond, stmt) {
			return true
		}
	case *syntax.WhileClause:
		if slices.Contains(owner.Cond, stmt) {
			return true
		}
	}
	for _, list := range stmtLists(owner) {
		i := slices.Index(list, stmt)
		if i < 0 {
			continue
		}
		return i == len(list)-1 || stmtReadsStatus(list[i+1])
	}
	return true
}

func stmtReadsStatus(stmt *syntax.Stmt) bool {
	reads := false
	parser.Walk(stmt, func(n syntax.Node) bool {
		if pe, ok := n.(*syntax.ParamExp); ok && pe.Param != nil && statusParameters[pe.Param.Value] {
			reads = true
		}
		return !reads
	})
	return reads
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
