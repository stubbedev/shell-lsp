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
	if !ok || p.flow == nil || p.flow.reached[stmt] {
		return
	}
	if _, body := p.parent().(*syntax.FuncDecl); body {
		return
	}
	if outer, ok := p.enclosingStmt(); ok && !p.flow.reached[outer] {
		return
	}
	p.reportNode(2317, stmt, "This command can never run, because every path to it exits, returns or loops forever first.")
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
	if !ok || p.flow == nil || !p.flow.deadFunctions[decl] {
		return
	}
	for depth := 0; p.ancestor(depth) != nil; depth++ {
		if outer, ok := p.ancestor(depth).(*syntax.FuncDecl); ok && p.flow.deadFunctions[outer] {
			return
		}
	}
	p.reportNode(2329, decl, "This function is never invoked, and the script cannot be sourced to call it later because it always exits first.")
}

func checkCallBeforeDefinition(p *pass, n syntax.Node) {
	call, ok := n.(*syntax.CallExpr)
	if !ok || !p.flow.forwardCall(call) {
		return
	}
	p.reportNode(2218, call, "This function is only defined further down, so it does not exist yet when this line runs; move the definition up.")
}

func checkStatusSource(p *pass, n syntax.Node) {
	pe, ok := n.(*syntax.ParamExp)
	if !ok || p.flow == nil || pe.Param == nil || pe.Param.Value != "?" {
		return
	}
	setters := p.flow.statusReads[pe]
	switch {
	case len(setters) == 0:
	case !slices.ContainsFunc(setters, func(s *syntax.Stmt) bool { return !isEcho(s) }):
		p.reportNode(2320, pe, "This $? is the status of echo, not of the command before it; save $? in a variable first.")
	case !slices.ContainsFunc(setters, func(s *syntax.Stmt) bool { return !isTestCommand(s) }) &&
		slices.ContainsFunc(setters, func(s *syntax.Stmt) bool { return p.flow.controls(s, pe) }):
		p.reportNode(2319, pe, "This $? is the status of the preceding test, not of the command it checked; save $? in a variable first.")
	}
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
	if !ok || !a.Append || p.flow == nil {
		return
	}
	if fact := p.flow.appends[a]; fact.numeric && !fact.integer {
		p.reportNode(2324, a, "+= on a variable appends text, so this produces a longer string rather than a sum; use (( var += n )) to add.")
	}
}
