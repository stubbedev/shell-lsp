package lint

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var structureFamily = family{
	codes: map[Code]meta{
		2249: {severity: SeverityInfo, optional: "add-default-case"},
		2262: {severity: SeverityWarning},
		2263: {severity: SeverityInfo},
		2264: {severity: SeverityError},
	},
	rules: []rule{
		{on: nodes(&syntax.CaseClause{}), node: checkDefaultCase},
		{on: nodes(&syntax.FuncDecl{}), node: checkSelfRecursion},
		{finish: checkAliasesInParsingUnit},
	},
}

func checkDefaultCase(p *pass, n syntax.Node) {
	cc, ok := n.(*syntax.CaseClause)
	if !ok {
		return
	}
	for _, item := range cc.Items {
		for _, pattern := range item.Patterns {
			if lit := pattern.Lit(); lit != "" && strings.Trim(lit, "*") == "" {
				return
			}
		}
	}
	p.reportNode(2249, cc, "This case statement has no *) branch; add one to handle unexpected values.")
}

func checkSelfRecursion(p *pass, n syntax.Node) {
	decl, ok := n.(*syntax.FuncDecl)
	if !ok || decl.Name == nil || decl.Body == nil {
		return
	}
	var first *syntax.Stmt
	switch body := decl.Body.Cmd.(type) {
	case *syntax.Block:
		if len(body.Stmts) > 0 {
			first = body.Stmts[0]
		}
	case *syntax.Subshell:
		if len(body.Stmts) > 0 {
			first = body.Stmts[0]
		}
	}
	if first == nil {
		return
	}
	for _, call := range unconditionalCalls(first, nil) {
		name := call.Args[0]
		if name.Lit() != decl.Name.Value {
			continue
		}
		start := parser.Offset(name.Pos())
		p.reportNode(2264, name, decl.Name.Value+" calls itself unconditionally, which recurses forever; use command "+decl.Name.Value+" to run the real command.",
			reviewFix("Call the command with command "+decl.Name.Value, textedit.Edit{Start: start, End: start, NewText: "command "}))
	}
}

func unconditionalCalls(stmt *syntax.Stmt, into []*syntax.CallExpr) []*syntax.CallExpr {
	if stmt.Negated {
		return into
	}
	switch cmd := stmt.Cmd.(type) {
	case *syntax.CallExpr:
		if len(cmd.Args) > 0 {
			return append(into, cmd)
		}
	case *syntax.BinaryCmd:
		into = unconditionalCalls(cmd.X, into)
		if pipes(cmd.Op) {
			return unconditionalCalls(cmd.Y, into)
		}
	}
	return into
}

type aliasDefinition struct {
	word *syntax.Word
	name string
}

type aliasUse struct {
	def  aliasDefinition
	call *syntax.CallExpr
}

func checkAliasesInParsingUnit(p *pass) {
	if !p.hasCommand("alias") {
		return
	}
	var uses []aliasUse
	stmts := p.tree.File.Stmts
	for start := 0; start < len(stmts); {
		end := start + 1
		for end < len(stmts) && p.sameLine(stmts[end-1].End(), stmts[end].Pos()) {
			end++
		}
		uses = append(uses, aliasUsesIn(stmts[start:end])...)
		start = end
	}
	if len(uses) == 0 {
		return
	}
	suppressed := p.suppressions()
	for _, use := range uses {
		span, ok := p.span(use.def.word)
		if !ok || suppressed.covers(Finding{Code: 2262, Span: span}) {
			continue
		}
		p.report(2262, span, "The alias "+use.def.name+" is used in the same parsing unit that defines it, where it does not take effect yet; define it on an earlier line or use a function.")
		p.reportNode(2263, use.call, "This runs the command "+use.def.name+", not the alias defined in the same parsing unit; move the alias to an earlier line or use a function.")
	}
}

func (p *pass) alias(name string) (aliasDefinition, bool) {
	if p.aliases == nil {
		p.aliases = map[string]aliasDefinition{}
		for _, c := range p.tree.Commands {
			if c.Name == "alias" {
				registerAliases(c.Args, p.aliases)
			}
		}
	}
	def, ok := p.aliases[name]
	return def, ok
}

func (p *pass) hasCommand(name string) bool {
	for _, c := range p.tree.Commands {
		if c.Name == name {
			return true
		}
	}
	return false
}

func (p *pass) sameLine(a, b syntax.Pos) bool {
	return p.tree.Line(parser.Offset(a)) == p.tree.Line(parser.Offset(b))
}

func aliasUsesIn(unit []*syntax.Stmt) []aliasUse {
	defined := map[string]aliasDefinition{}
	var uses []aliasUse
	for _, stmt := range unit {
		parser.Walk(stmt, func(n syntax.Node) bool {
			call, ok := n.(*syntax.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			name, ok := unquotedName(call.Args[0])
			if !ok {
				return true
			}
			if name == "alias" {
				registerAliases(call.Args[1:], defined)
				return true
			}
			if def, ok := defined[name]; ok && !strings.Contains(name, "/") {
				uses = append(uses, aliasUse{def: def, call: call})
			}
			return true
		})
	}
	return uses
}

func unquotedName(w *syntax.Word) (string, bool) {
	if len(w.Parts) != 1 {
		return "", false
	}
	lit, ok := w.Parts[0].(*syntax.Lit)
	if !ok {
		return "", false
	}
	return lit.Value, true
}

func registerAliases(args []*syntax.Word, defined map[string]aliasDefinition) {
	for _, arg := range args {
		text, _ := parser.Text(arg, "-")
		name, value, found := strings.Cut(text, "=")
		if !found || value == "" || !parser.IsName(name) {
			continue
		}
		if _, seen := defined[name]; !seen {
			defined[name] = aliasDefinition{word: arg, name: name}
		}
	}
}
