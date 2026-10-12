package lint

import (
	"path"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var errexitFamily = family{
	codes: map[Code]meta{
		2251: {severity: SeverityInfo, bug: true, summary: "A negated command never triggers set -e."},
		2310: {severity: SeverityInfo, optional: optionalSetESuppressed, summary: "set -e has no effect inside a function invoked as a condition."},
		2311: {severity: SeverityInfo, optional: optionalSetESuppressed, summary: "set -e has no effect inside a command substitution that does not inherit it."},
		2312: {severity: SeverityInfo, optional: optionalMaskedReturns, summary: "A command's exit status is masked by its context."},
	},
	nodes: []nodeRule{
		onNode(checkNegationUnderErrexit),
		onNode(checkSuppressedErrexit),
		onNodes(checkMaskedReturns, &syntax.ArithmCmd{}, &syntax.TestClause{}, &syntax.ProcSubst{}, &syntax.ArrayExpr{}, &syntax.DblQuoted{}, &syntax.Word{}, &syntax.Redirect{}, &syntax.BinaryCmd{}, &syntax.CallExpr{}, &syntax.DeclClause{}),
	},
}

func checkNegationUnderErrexit(p *pass, stmt *syntax.Stmt) {
	if !stmt.Negated || !p.statusIgnored(stmt) || p.inCondition(stmt) || !p.options().errexit {
		return
	}
	start := parser.Offset(stmt.Pos())
	command := parser.Offset(stmt.Cmd.Pos())
	end := commandEnd(stmt)
	p.report(2251, posSpan(stmt.Pos(), 1), "A negated command never triggers set -e, so this check has no effect; make it fail explicitly, e.g. with && exit 1.",
		reviewFix("Replace the ! with && exit 1", textedit.Edit{Start: start, End: command}, textedit.Edit{Start: end, End: end, NewText: " && exit 1"}))
}

func commandEnd(stmt *syntax.Stmt) int {
	end := parser.Offset(stmt.Cmd.End())
	for _, r := range stmt.Redirs {
		end = max(end, parser.Offset(r.End()))
	}
	return end
}

func (p *pass) statusIgnored(stmt *syntax.Stmt) bool {
	switch owner := p.parent().(type) {
	case *syntax.File:
		return !isLast(owner.Stmts, stmt)
	case *syntax.Block:
		_, function := p.ancestor(2).(*syntax.FuncDecl)
		return !function || !isLast(owner.Stmts, stmt)
	case *syntax.Subshell:
		return !isLast(owner.Stmts, stmt)
	case *syntax.WhileClause:
		return !isLast(owner.Cond, stmt)
	case *syntax.IfClause:
		return !isLast(owner.Cond, stmt)
	case *syntax.ForClause, *syntax.RepeatClause:
		return true
	}
	return false
}

func isLast(list []*syntax.Stmt, stmt *syntax.Stmt) bool {
	return len(list) > 0 && list[len(list)-1] == stmt
}

func (p *pass) inCondition(stmt *syntax.Stmt) bool {
	var child syntax.Node = stmt
	for depth := 0; ; depth++ {
		switch owner := p.ancestor(depth).(type) {
		case nil:
			return false
		case *syntax.BinaryCmd:
			if (owner.Op == syntax.AndStmt || owner.Op == syntax.OrStmt) && owner.X == child {
				return true
			}
		case *syntax.IfClause:
			if isLastNode(owner.Cond, child) {
				return true
			}
		case *syntax.WhileClause:
			if isLastNode(owner.Cond, child) {
				return true
			}
		case *syntax.TestDecl:
			return true
		}
		child = p.ancestor(depth)
	}
}

func isLastNode(list []*syntax.Stmt, n syntax.Node) bool {
	stmt, ok := n.(*syntax.Stmt)
	return ok && isLast(list, stmt)
}

func containsNode(list []*syntax.Stmt, n syntax.Node) bool {
	stmt, ok := n.(*syntax.Stmt)
	return ok && slices.Contains(list, stmt)
}

func checkSuppressedErrexit(p *pass, call *syntax.CallExpr) {
	if len(call.Args) == 0 || !p.settings.enabled(optionalSetESuppressed) {
		return
	}
	name := call.Args[0]
	lit, ok := name.Parts[0].(*syntax.Lit)
	if !ok || len(name.Parts) != 1 || !p.options().errexit {
		return
	}
	if _, defined := p.tree.FunctionNamed(lit.Value); !defined {
		return
	}
	var child syntax.Node = call
	for depth := 0; p.ancestor(depth) != nil; depth++ {
		if context, ok := p.errexitSuppression(p.ancestor(depth), child); ok {
			p.reportNode(2310, name, "set -e has no effect inside "+lit.Value+" because it is invoked as "+context+"; check its status explicitly if that matters.")
		}
		if cs, ok := p.ancestor(depth).(*syntax.CmdSubst); ok && p.losesErrexit(cs) {
			p.reportNode(2311, name, "set -e has no effect inside "+lit.Value+" because command substitutions do not inherit it; enable shopt -s inherit_errexit or call set -e inside.")
		}
		child = p.ancestor(depth)
	}
}

func (p *pass) errexitSuppression(owner, child syntax.Node) (string, bool) {
	switch owner := owner.(type) {
	case *syntax.Stmt:
		return "a negated command", owner.Negated
	case *syntax.BinaryCmd:
		switch {
		case owner.X != child:
		case owner.Op == syntax.AndStmt:
			return "the left side of &&", true
		case owner.Op == syntax.OrStmt:
			return "the left side of ||", true
		}
	case *syntax.IfClause:
		return "an if condition", containsNode(owner.Cond, child)
	case *syntax.WhileClause:
		if owner.Until {
			return "an until condition", containsNode(owner.Cond, child)
		}
		return "a while condition", containsNode(owner.Cond, child)
	}
	return "", false
}

func (p *pass) losesErrexit(cs *syntax.CmdSubst) bool {
	if cs.TempFile || cs.ReplyVar || p.options().inheritErrexit {
		return false
	}
	switch p.dialect {
	case dialect.Bash, dialect.Ksh, dialect.Zsh:
	default:
		return false
	}
	return !parser.Any(cs, func(n syntax.Node) bool {
		call, ok := n.(*syntax.CallExpr)
		if !ok {
			return false
		}
		c, ok := commandOf(call)
		return ok && turnsOn(optionChanges(c.name, c.args, p.dialect), "errexit")
	})
}

var harmlessCommands = setOf("echo", "basename", "dirname", "printf", "set", "shopt")

func checkMaskedReturns(p *pass, n syntax.Node) {
	if !p.settings.enabled(optionalMaskedReturns) {
		return
	}
	for _, unit := range p.maskedUnits(n) {
		p.reportMaskedIn(n, unit)
	}
}

func (p *pass) maskedUnits(n syntax.Node) []syntax.Node {
	switch n := n.(type) {
	case *syntax.ArithmCmd:
		return []syntax.Node{n.X}
	case *syntax.TestClause:
		return []syntax.Node{n.X}
	case *syntax.ProcSubst:
		units := make([]syntax.Node, 0, len(n.Stmts))
		for _, s := range n.Stmts {
			units = append(units, s)
		}
		return units
	case *syntax.ArrayExpr:
		return allButLastWithCommand(n.Elems, func(e *syntax.ArrayElem) syntax.Node { return e })
	case *syntax.DblQuoted:
		return allButLastWithCommand(n.Parts, func(part syntax.WordPart) syntax.Node { return part })
	case *syntax.Word:
		return allButLastWithCommand(n.Parts, func(part syntax.WordPart) syntax.Node { return part })
	case *syntax.Redirect:
		switch {
		case n.Hdoc != nil:
			return []syntax.Node{n.Hdoc}
		case n.Op == syntax.WordHdoc:
			return []syntax.Node{n.Word}
		}
	case *syntax.BinaryCmd:
		if pipes(n.Op) && !p.options().pipefail && !p.insidePipeline() {
			return allButLastWithCommand(pipelineStages(&syntax.Stmt{Cmd: n}, nil), func(s *syntax.Stmt) syntax.Node { return s })
		}
	case *syntax.CallExpr:
		return p.commandOperands(n)
	case *syntax.DeclClause:
		units := make([]syntax.Node, 0, len(n.Args))
		for _, a := range n.Args {
			units = append(units, a)
		}
		return units
	}
	return nil
}

func (p *pass) insidePipeline() bool {
	bin, ok := p.ancestor(1).(*syntax.BinaryCmd)
	return ok && pipes(bin.Op)
}

func (p *pass) commandOperands(call *syntax.CallExpr) []syntax.Node {
	words := timelessWords(call)
	if len(words) == 0 {
		return allButLastWithCommand(call.Assigns, func(a *syntax.Assign) syntax.Node { return a })
	}
	units := make([]syntax.Node, 0, len(call.Assigns)+len(words)-1)
	for _, a := range call.Assigns {
		units = append(units, a)
	}
	for _, w := range words[1:] {
		units = append(units, w)
	}
	return units
}

func timelessWords(call *syntax.CallExpr) []*syntax.Word {
	if name, ok := parser.CallName(call); ok && path.Base(name) == "time" {
		return call.Args[1:]
	}
	return call.Args
}

func allButLastWithCommand[T any](items []T, node func(T) syntax.Node) []syntax.Node {
	var out []syntax.Node
	for _, item := range items {
		if n := node(item); containsCommand(n) {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out[:len(out)-1]
}

func containsCommand(root syntax.Node) bool {
	return parser.Any(root, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.CallExpr:
			return len(n.Args) > 0
		case *syntax.TestClause:
			return true
		}
		return false
	})
}

func (p *pass) reportMaskedIn(context, unit syntax.Node) {
	if p.maskWalked[unit] {
		return
	}
	if p.maskWalked == nil {
		p.maskWalked = map[syntax.Node]bool{}
	}
	var guards maskGuards
	for _, n := range p.stack {
		guards.push(n)
	}
	guards.push(context)
	parser.Walk(unit, func(n syntax.Node) bool {
		if n == nil {
			guards.pop()
			return true
		}
		p.maskWalked[n] = true
		if isMaskableCommand(n) && guards.declaring == 0 && guards.ignoring == 0 && !ignoresFailure(n) {
			p.reportNode(2312, n, "This command's exit status is masked by its context; run it separately or append || true if failure does not matter.")
		}
		guards.push(n)
		return true
	})
}

type maskGuards struct {
	pushed    []maskGuard
	declaring int
	ignoring  int
}

type maskGuard struct{ declaring, ignoring bool }

func (g *maskGuards) push(n syntax.Node) {
	guard := maskGuard{declaring: isDeclaringCommand(n), ignoring: ignoresFailure(n)}
	g.pushed = append(g.pushed, guard)
	g.count(guard, 1)
}

func (g *maskGuards) pop() {
	last := len(g.pushed) - 1
	g.count(g.pushed[last], -1)
	g.pushed = g.pushed[:last]
}

func (g *maskGuards) count(guard maskGuard, delta int) {
	if guard.declaring {
		g.declaring += delta
	}
	if guard.ignoring {
		g.ignoring += delta
	}
}

func isMaskableCommand(n syntax.Node) bool {
	switch n := n.(type) {
	case *syntax.TestClause:
		return true
	case *syntax.TimeClause:
		return n.Stmt != nil && !isHarmless(n.Stmt.Cmd)
	case *syntax.CallExpr:
		return !isHarmless(n)
	}
	return false
}

func isHarmless(cmd syntax.Command) bool {
	call, ok := cmd.(*syntax.CallExpr)
	if !ok {
		return false
	}
	words := timelessWords(call)
	if len(words) == 0 {
		return true
	}
	lit, ok := parser.Literal(words[0])
	return ok && harmlessCommands[path.Base(lit)]
}

func isDeclaringCommand(n syntax.Node) bool {
	switch n := n.(type) {
	case *syntax.DeclClause:
		if n.Variant.Value != "local" {
			return declaringOrLet[n.Variant.Value]
		}
		return !slices.ContainsFunc(n.Args, func(a *syntax.Assign) bool {
			lit, ok := parser.Literal(a.Value)
			return a.Naked && ok && len(lit) > 1 && lit[0] == '-' && slices.Contains([]byte(lit[1:]), 'r')
		})
	case *syntax.LetClause:
		return true
	case *syntax.CallExpr:
		c, ok := commandOf(n)
		return ok && declaringOrLet[path.Base(c.name)] && c.name != "local"
	}
	return false
}

func ignoresFailure(n syntax.Node) bool {
	bin, ok := n.(*syntax.BinaryCmd)
	if !ok || bin.Op != syntax.OrStmt {
		return false
	}
	call, ok := bin.Y.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	name, _ := parser.CallName(call)
	return name == "true" || name == ":"
}
