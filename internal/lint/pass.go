package lint

import (
	"fmt"
	"runtime/debug"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

type rule struct {
	on     []nodeKind
	node   func(p *pass, n syntax.Node)
	finish func(p *pass)
}

type pass struct {
	tree         *parser.Tree
	imports      []parser.Import
	sources      []Source
	text         string
	dialect      dialect.Dialect
	bats         bool
	stack        []syntax.Node
	stmts        []parser.Span
	flowChecks   []func(flow *flowResult)
	statements   []nestedStmt
	catalog      map[Code]meta
	rules        []rule
	nodeChecks   nodeDispatch
	commands     commandDispatch
	settings     fileWide
	findings     []Finding
	aliases      map[string]aliasDefinition
	quoteTaint   map[string]parser.Span
	firstCommand int
	directives   []directiveComment

	shellChosen    bool
	shellOptions   *shellOptions
	variables      variableUsage
	associative    map[string]bool
	dialectGuessed bool
	emptinessTests map[int]bool
}

func newPass(tree *parser.Tree, imports []parser.Import, opts Options) *pass {
	directives := parseDirectives(tree)
	firstCommand := firstCommandStart(tree)
	return &pass{
		tree: tree, imports: imports, sources: opts.Sources, text: tree.Text(), dialect: tree.Dialect, bats: parser.IsBats(tree.Text()),
		firstCommand: firstCommand, directives: directives, catalog: catalog, rules: rules, nodeChecks: nodeChecks, commands: commandChecks,
		settings: fileWideSettings(directives, firstCommand, opts.Optional),
	}
}

func (p *pass) run() {
	flow := p.startFlow()
	parser.Walk(p.tree.File, func(n syntax.Node) bool {
		if n == nil {
			p.stack = p.stack[:len(p.stack)-1]
			return true
		}
		if stmt, ok := n.(*syntax.Stmt); ok {
			if span, ok := p.tree.NodeSpan(stmt); ok {
				p.stmts = append(p.stmts, span)
			}
		}
		for _, check := range p.nodeChecks[nodeKindOf(n)] {
			check(p, n)
		}
		p.commands.visit(p, n)
		p.stack = append(p.stack, n)
		return true
	})
	result := flow()
	for _, check := range p.flowChecks {
		check(result)
	}
	for _, r := range p.rules {
		if r.finish != nil {
			r.finish(p)
		}
	}
}

type flowOutcome struct {
	result  *flowResult
	failure string
}

func (p *pass) startFlow() func() *flowResult {
	if !p.settings.extended {
		return func() *flowResult { return nil }
	}
	done := make(chan flowOutcome, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- flowOutcome{failure: fmt.Sprintf("%v\n%s", r, debug.Stack())}
			}
		}()
		done <- flowOutcome{result: analyzeFlow(p.tree, p.imports, p.bats)}
	}()
	return func() *flowResult {
		outcome := <-done
		if outcome.failure != "" {
			panic("lint: flow analysis failed: " + outcome.failure)
		}
		return outcome.result
	}
}

func (p *pass) withFlow(check func(flow *flowResult)) {
	if !p.settings.extended {
		check(nil)
		return
	}
	p.flowChecks = append(p.flowChecks, check)
}

func (p *pass) parent() syntax.Node { return p.ancestor(0) }

func (p *pass) ancestor(depth int) syntax.Node {
	i := len(p.stack) - 1 - depth
	if i < 0 {
		return nil
	}
	return p.stack[i]
}

func (p *pass) span(n syntax.Node) (parser.Span, bool) { return p.tree.NodeSpan(n) }

func (p *pass) reportNode(code Code, n syntax.Node, message string, fix ...textedit.Edit) {
	if span, ok := p.span(n); ok {
		p.report(code, span, message, fix...)
	}
}

func (p *pass) report(code Code, span parser.Span, message string, fix ...textedit.Edit) {
	p.reportWithSeverity(code, p.meta(code).severity, span, message, fix...)
}

func (p *pass) reportWithSeverity(code Code, severity Severity, span parser.Span, message string, fix ...textedit.Edit) {
	m := p.meta(code)
	if m.optional != "" && !p.settings.enabled(m.optional) {
		return
	}
	p.findings = append(p.findings, Finding{Code: code, Severity: severity, Tag: m.tag, Span: span, Message: message, Fix: fix})
}

func (p *pass) meta(code Code) meta {
	m, ok := p.catalog[code]
	if !ok {
		panic(fmt.Sprintf("lint: %s is reported but missing from the catalog", code))
	}
	return m
}

func (p *pass) rootParseError() (parser.Error, bool) {
	if len(p.tree.Errors) == 0 {
		return parser.Error{}, false
	}
	return p.tree.Errors[0], true
}
