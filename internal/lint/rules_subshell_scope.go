package lint

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/getopt"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const (
	codeSubshellTrap   = nativeBase + 2004
	codeSubshellExit   = nativeBase + 2005
	codeUnbalancedPush = nativeBase + 2007
)

var subshellScopeFamily = family{
	codes: map[Code]meta{
		codeSubshellTrap:   {severity: SeverityWarning},
		codeSubshellExit:   {severity: SeverityWarning},
		codeUnbalancedPush: {severity: SeverityInfo, bug: true},
	},
	commands: []commandRule{
		{match: matchExact, names: []string{"trap"}, check: checkSubshellTrap},
		{match: matchExact, names: []string{"exit"}, check: checkSubshellExit},
		{match: matchExact, names: []string{"pushd"}, check: checkUnbalancedPush},
	},
}

func checkUnbalancedPush(p *pass, c command) {
	if !changesDirectory(c.args) {
		return
	}
	at, ok := p.enclosure()
	if !ok || at.subshell != notSubshell {
		return
	}
	fn, ok := p.ancestor(at.depth).(*syntax.FuncDecl)
	if !ok || p.callsCommand(fn.Body, "popd") || p.popsElsewhere() {
		return
	}
	p.reportNode(codeUnbalancedPush, c.word, fn.Name.Value+" runs pushd but never popd, so every call leaves its caller in another directory with one more entry on the directory stack.")
}

func changesDirectory(args []*syntax.Word) bool {
	for _, arg := range args {
		lit, ok := parser.Literal(arg)
		switch {
		case !ok:
			return true
		case lit == "-n" || ((strings.HasPrefix(lit, "+") || strings.HasPrefix(lit, "-")) && len(lit) > 1 && parser.IsDigits(lit[1:])):
			return false
		case lit == getopt.EndOfOptions:
		default:
			return true
		}
	}
	return false
}

func (p *pass) popsElsewhere() bool {
	pops := false
	parser.Walk(p.tree.File, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.FuncDecl:
			return !p.callsCommand(n.Body, "pushd")
		case *syntax.CallExpr:
			if command, ok := p.runs(n.Args); ok && command.name == "popd" {
				pops = true
			}
		}
		return !pops
	})
	return pops
}

func (p *pass) callsCommand(root syntax.Node, name string) bool {
	return parser.Any(root, func(n syntax.Node) bool {
		call, ok := n.(*syntax.CallExpr)
		if !ok {
			return false
		}
		command, ok := p.runs(call.Args)
		return ok && command.name == name
	})
}

var (
	trapQueries      = setOf("-p", "-l", "-P")
	statusParameters = setOf("?", "PIPESTATUS", "pipestatus")
)

func checkSubshellTrap(p *pass, c command) {
	handler, signals, ok := trapOperands(c.args)
	if !ok || len(signals) == 0 {
		return
	}
	if lit, literal := parser.Literal(handler); literal && lit == "-" {
		return
	}
	at, ok := p.enclosure()
	if !ok || !at.subshell.implicit() {
		return
	}
	p.reportNode(codeSubshellTrap, c.word, "This trap is set inside a "+at.subshell.label()+", which runs in a subshell, so the handler only applies until that subshell ends and never to the rest of the script.")
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
	at, ok := p.enclosure()
	if !ok || !at.subshell.implicit() {
		return
	}
	var lost bool
	switch n := p.ancestor(at.depth).(type) {
	case *syntax.CmdSubst:
		lost = p.substitutionExitLost(at.depth)
	case *syntax.Stmt:
		lost = p.pipelineExitLost(n, at.depth)
	}
	if lost {
		kind := at.subshell.label()
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
	if stages[len(stages)-1] != stage && !p.optionsAt(pipeline).Pipefail {
		return true
	}
	return !p.statusChecked(pipeline, outer)
}

func (p *pass) statusChecked(stmt *syntax.Stmt, depth int) bool {
	if stmt.Negated || p.optionsAt(stmt).Errexit {
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
	return parser.Any(stmt, func(n syntax.Node) bool {
		pe, ok := n.(*syntax.ParamExp)
		return ok && pe.Param != nil && statusParameters[pe.Param.Value]
	})
}

type enclosure struct {
	subshell subshellKind
	depth    int
}

func (p *pass) enclosure() (enclosure, bool) {
	for depth := 0; p.ancestor(depth) != nil; depth++ {
		n := p.ancestor(depth)
		if _, function := n.(*syntax.FuncDecl); function {
			return enclosure{subshell: notSubshell, depth: depth}, true
		}
		if kind := p.subshellKind(n, depth); kind != notSubshell {
			return enclosure{subshell: kind, depth: depth}, true
		}
	}
	return enclosure{}, false
}
