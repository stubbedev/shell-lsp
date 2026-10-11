package lint

import (
	"path"
	"slices"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/parser"
)

var flowFamily = family{
	codes: map[Code]meta{
		2218: {severity: SeverityError},
		2317: {severity: SeverityInfo},
		2319: {severity: SeverityWarning},
		2320: {severity: SeverityWarning},
		2324: {severity: SeverityWarning},
		2329: {severity: SeverityInfo},
	},
	rules: []rule{
		{on: nodes(&syntax.Stmt{}), node: checkUnreachable},
		{on: nodes(&syntax.FuncDecl{}), node: checkUninvokedFunction},
		{on: nodes(&syntax.CallExpr{}), node: checkCallBeforeDefinition},
		{on: nodes(&syntax.ParamExp{}), node: checkStatusSource},
		{on: nodes(&syntax.Assign{}), node: checkNumericAppend},
	},
}

func checkUnreachable(p *pass, n syntax.Node) {
	stmt, ok := n.(*syntax.Stmt)
	if !ok || !p.settings.extended {
		return
	}
	switch p.parent().(type) {
	case *syntax.FuncDecl, *syntax.ArrayExpr:
		return
	}
	if len(p.statements) == 0 {
		p.withFlow(p.reportUnreachable)
	}
	outer, _ := p.enclosingStmt()
	p.statements = append(p.statements, nestedStmt{stmt: stmt, outer: outer})
}

type nestedStmt struct {
	stmt  *syntax.Stmt
	outer *syntax.Stmt
}

func (p *pass) reportUnreachable(flow *flowResult) {
	for _, s := range p.statements {
		if flow.reached[s.stmt] || s.outer != nil && !flow.reached[s.outer] {
			continue
		}
		p.reportNode(2317, s.stmt, "This command can never run, because every path to it exits, returns or loops forever first.")
	}
}

func (p *pass) enclosingStmt() (*syntax.Stmt, bool) {
	for depth := 0; ; depth++ {
		switch n := p.ancestor(depth).(type) {
		case nil:
			return nil, false
		case *syntax.Stmt:
			return n, true
		}
	}
}

func checkUninvokedFunction(p *pass, n syntax.Node) {
	decl, ok := n.(*syntax.FuncDecl)
	if !ok || !p.settings.extended {
		return
	}
	var outers []*syntax.FuncDecl
	for depth := 0; p.ancestor(depth) != nil; depth++ {
		if outer, ok := p.ancestor(depth).(*syntax.FuncDecl); ok {
			outers = append(outers, outer)
		}
	}
	p.withFlow(func(flow *flowResult) {
		if !flow.deadFunctions[decl] || slices.ContainsFunc(outers, func(outer *syntax.FuncDecl) bool { return flow.deadFunctions[outer] }) {
			return
		}
		p.reportNode(2329, decl, "This function is never invoked, and the script cannot be sourced to call it later because it always exits first.")
	})
}

func checkCallBeforeDefinition(p *pass, n syntax.Node) {
	call, ok := n.(*syntax.CallExpr)
	if !ok || !p.settings.extended || len(call.Args) == 0 {
		return
	}
	name, ok := parser.Literal(call.Args[0])
	if !ok {
		return
	}
	if _, defined := p.tree.FunctionNamed(name); !defined {
		return
	}
	p.withFlow(func(flow *flowResult) {
		if flow.forwardCall(call) {
			p.reportNode(2218, call, "This function is only defined further down, so it does not exist yet when this line runs; move the definition up.")
		}
	})
}

func checkStatusSource(p *pass, n syntax.Node) {
	pe, ok := n.(*syntax.ParamExp)
	if !ok || !p.settings.extended || pe.Param == nil || pe.Param.Value != "?" {
		return
	}
	p.withFlow(func(flow *flowResult) {
		setters := flow.statusReads[pe]
		switch {
		case len(setters) == 0:
		case !slices.ContainsFunc(setters, func(s *syntax.Stmt) bool { return !isEcho(s) }):
			p.reportNode(2320, pe, "This $? is the status of echo, not of the command before it; save $? in a variable first.")
		case !slices.ContainsFunc(setters, func(s *syntax.Stmt) bool { return !isTestCommand(s) }) &&
			slices.ContainsFunc(setters, func(s *syntax.Stmt) bool { return flow.controls(s, pe) }):
			p.reportNode(2319, pe, "This $? is the status of the preceding test, not of the command it checked; save $? in a variable first.")
		}
	})
}

func (r *flowResult) controls(setter *syntax.Stmt, pe *syntax.ParamExp) bool {
	region, ok := r.conditional[setter]
	return ok && region.Contains(parser.Offset(pe.Pos()))
}

func isEcho(stmt *syntax.Stmt) bool {
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok {
		return false
	}
	c, ok := commandOf(call)
	return ok && path.Base(c.name) == "echo"
}

func checkNumericAppend(p *pass, n syntax.Node) {
	a, ok := n.(*syntax.Assign)
	if !ok || !a.Append || !p.settings.extended {
		return
	}
	p.withFlow(func(flow *flowResult) {
		if fact := flow.appends[a]; fact.numeric && !fact.integer {
			p.reportNode(2324, a, "+= on a variable appends text, so this produces a longer string rather than a sum; use (( var += n )) to add.")
		}
	})
}
