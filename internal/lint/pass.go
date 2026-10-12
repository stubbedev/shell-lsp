package lint

import (
	"fmt"
	"runtime/debug"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/shelldocs"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type rule struct {
	on     []nodeKind
	node   func(p *pass, n syntax.Node)
	finish func(p *pass)
}

type pass struct {
	tree            *parser.Tree
	imports         []parser.Import
	sources         []Source
	text            string
	dialect         dialect.Dialect
	bats            bool
	stack           []syntax.Node
	stmts           []parser.Span
	flowChecks      []func(flow *flowResult)
	statements      []nestedStmt
	untrustedValues *untrustedValues
	nameReads       *nameReads
	catalog         map[Code]meta
	rules           []rule
	nodeChecks      nodeDispatch
	commands        commandDispatch
	settings        fileWide
	findings        []Finding
	aliases         map[string]aliasDefinition
	quoteTaint      map[string]parser.Span
	firstCommand    int
	directives      []directiveComment

	rcFile         bool
	shellChosen    bool
	shellOptions   *shellOptions
	variables      variableUsage
	parameters     shelldocs.ParameterTable
	dialectGuessed bool
	emptinessTests map[int]bool
	maskWalked     map[syntax.Node]bool
	braceScan      *braceScanner
	executed       *bool
	lastResolved   resolvedCall
	quotedHeredocs map[int]bool

	normalized Normalized
	probes     []Finding
}

func newPass(tree *parser.Tree, imports []parser.Import, opts Options) *pass {
	directives := parseDirectives(tree)
	firstCommand := firstCommandStart(tree)
	return &pass{
		tree: tree, imports: imports, sources: opts.Sources, rcFile: opts.RCFile, text: tree.Text(), dialect: tree.Dialect, bats: tree.Bats(),
		firstCommand: firstCommand, directives: directives, catalog: catalog, rules: rules, nodeChecks: nodeChecks, commands: commandChecks,
		settings:   fileWideSettings(directives, firstCommand, opts),
		stmts:      make([]parser.Span, 0, tree.Statements),
		variables:  variableUsage{reads: make([]variableRead, 0, len(tree.References))},
		parameters: shelldocs.KnownParameters(tree.Dialect),

		normalized: opts.Normalized,
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

const maxExcerpt = 120

func (p *pass) excerpt(span parser.Span) string {
	text := p.text[span.Start:span.End]
	if len(text) <= maxExcerpt {
		return text
	}
	cut := maxExcerpt
	for !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "…"
}

func (p *pass) reportNode(code Code, n syntax.Node, message string, fixes ...Fix) {
	if span, ok := p.span(n); ok {
		p.report(code, span, message, fixes...)
	}
}

func (p *pass) report(code Code, span parser.Span, message string, fixes ...Fix) {
	p.reportWithSeverity(code, p.meta(code).severity, span, message, fixes...)
}

func (p *pass) reportWithSeverity(code Code, severity Severity, span parser.Span, message string, fixes ...Fix) {
	if !p.active(code) {
		return
	}
	p.findings = append(p.findings, Finding{Code: code, Severity: severity, Tag: p.meta(code).tag, Span: span, Message: message, Fixes: fixes})
}

func (p *pass) profileKeeps(f Finding) bool {
	m := p.meta(f.Code)
	return p.settings.profile.keeps(f, m, m.optional != "" && p.settings.chosen(m.optional))
}

func (p *pass) active(code Code) bool {
	m := p.meta(code)
	return m.optional == "" || p.settings.enabled(m.optional)
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
