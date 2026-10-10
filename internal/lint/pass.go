package lint

import (
	"fmt"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

type rule struct {
	node   func(p *pass, n syntax.Node)
	finish func(p *pass)
}

type pass struct {
	tree     *parser.Tree
	imports  []parser.Import
	text     string
	dialect  dialect.Dialect
	bats     bool
	stack    []syntax.Node
	stmts    []parser.Span
	flow     *flowResult
	findings []Finding
}

func newPass(tree *parser.Tree, imports []parser.Import) *pass {
	return &pass{tree: tree, imports: imports, text: tree.Text(), dialect: tree.Dialect, bats: isBats(tree.Text())}
}

func (p *pass) run() {
	p.flow = analyzeFlow(p.tree, p.imports, p.bats)
	syntax.Walk(p.tree.File, func(n syntax.Node) bool {
		if n == nil {
			p.stack = p.stack[:len(p.stack)-1]
			return true
		}
		if stmt, ok := n.(*syntax.Stmt); ok {
			if span, ok := p.tree.NodeSpan(stmt); ok {
				p.stmts = append(p.stmts, span)
			}
		}
		for _, r := range rules {
			if r.node != nil {
				r.node(p, n)
			}
		}
		p.stack = append(p.stack, n)
		return true
	})
	for _, r := range rules {
		if r.finish != nil {
			r.finish(p)
		}
	}
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

func (p *pass) report(code Code, span parser.Span, message string, fix ...textedit.Edit) {
	m, ok := catalog[code]
	if !ok {
		panic(fmt.Sprintf("lint: %s is reported but missing from the catalog", code))
	}
	p.findings = append(p.findings, Finding{Code: code, Severity: m.severity, Tag: m.tag, Span: span, Message: message, Fix: fix})
}

func isBats(text string) bool {
	first, _, _ := strings.Cut(text, "\n")
	return strings.HasPrefix(first, "#!") && strings.Contains(first, "bats")
}
