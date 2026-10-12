package lint

import (
	"path"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var flowFamily = family{
	codes: map[Code]meta{
		2218: {severity: SeverityError, summary: "A function is called before the line that defines it runs."},
		2317: {severity: SeverityInfo, summary: "A command can never run, because every path to it exits, returns or loops forever first."},
		2319: {severity: SeverityWarning, summary: "$? after a test is the status of that test, not of the command it checked."},
		2320: {severity: SeverityWarning, summary: "$? after echo is the status of echo, not of the command before it."},
		2324: {severity: SeverityWarning, summary: "+= on a plain variable appends text instead of adding a number."},
		2329: {severity: SeverityInfo, summary: "A function is never invoked, and the script always exits before it could be sourced and called."},
	},
	nodes: []nodeRule{
		onNode(checkUnreachable),
		onNode(checkUninvokedFunction),
		onNode(checkCallBeforeDefinition),
		onNode(checkStatusSource),
		onNode(checkNumericAppend),
	},
}

func checkUnreachable(p *pass, stmt *syntax.Stmt) {
	if !p.settings.extended {
		return
	}
	switch p.parent().(type) {
	case *syntax.FuncDecl, *syntax.ArrayExpr:
		return
	}
	if p.statements == nil {
		p.statements = make([]nestedStmt, 0, p.tree.Statements)
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

func checkUninvokedFunction(p *pass, decl *syntax.FuncDecl) {
	if !p.settings.extended {
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

func checkCallBeforeDefinition(p *pass, call *syntax.CallExpr) {
	if !p.settings.extended {
		return
	}
	name, ok := parser.CallName(call)
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

func checkStatusSource(p *pass, pe *syntax.ParamExp) {
	if !p.settings.extended || pe.Param == nil || pe.Param.Value != "?" {
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
	return ok && region.Touches(parser.Offset(pe.Pos()))
}

func isEcho(stmt *syntax.Stmt) bool {
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok {
		return false
	}
	c, ok := commandOf(call)
	return ok && path.Base(c.name) == "echo"
}

func checkNumericAppend(p *pass, a *syntax.Assign) {
	if !a.Append || !p.settings.extended {
		return
	}
	p.withFlow(func(flow *flowResult) {
		if fact := flow.appends[a]; fact.numeric && !fact.integer {
			p.reportNode(2324, a, "+= on a variable appends text, so this produces a longer string rather than a sum; use (( var += n )) to add.")
		}
	})
}
