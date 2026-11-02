package nix

import (
	"cmp"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

type Document struct {
	tree  *syntax.File
	table *scope.Table
}

func Read(src string) *Document {
	tree := syntax.Parse(src)
	return &Document{tree: tree, table: scope.Resolve(tree)}
}

func (d *Document) inspect(visit func(syntax.Node) bool) {
	syntax.Inspect(d.tree.Root, visit)
	for _, stray := range d.tree.Stray {
		syntax.Inspect(stray, visit)
	}
}

func (d *Document) text(span model.Span) string { return d.tree.Text[span.Start:span.End] }

func (d *Document) localFlow() (*Project, *symbolTable) {
	ix := &model.Index{}
	unit := newSymbolTable(&file{src: d.tree.Text, tree: d.tree, table: d.table}, ix)
	unit.build()
	return standalone(ix), unit
}

type ParameterHint struct {
	Offset int
	Name   string
}

func (d *Document) ParameterHints(within model.Span) []ParameterHint {
	p, u := d.localFlow()
	var out []ParameterHint
	d.inspect(func(n syntax.Node) bool {
		if call, ok := n.(*syntax.Apply); ok {
			out = d.callHints(out, p, u, call, within)
		}
		return true
	})
	slices.SortStableFunc(out, func(a, b ParameterHint) int { return cmp.Compare(a.Offset, b.Offset) })
	return out
}

func (d *Document) callHints(out []ParameterHint, p *Project, u *symbolTable, call *syntax.Apply, within model.Span) []ParameterHint {
	fn := syntax.Unparenthesized(call.Fn)
	if sel, selected := fn.(*syntax.Select); selected && sel.Default != nil {
		return out
	}
	lambda, known := p.onlyLambda(u, fn)
	for _, arg := range call.Args {
		if !known {
			return out
		}
		span := arg.Span()
		if param, ok := lambda.fn.Param.(*syntax.IdentParam); ok && within.Start <= span.Start && span.End <= within.End && d.text(span) != param.Name.Name {
			out = append(out, ParameterHint{Offset: span.Start, Name: param.Name.Name})
		}
		lambda, known = p.onlyLambda(lambda.unit, lambda.fn.Body)
	}
	return out
}

func (p *Project) onlyLambda(u *symbolTable, e syntax.Expr) (flowLambda, bool) {
	p.begin()
	defer p.mu.Unlock()
	values := p.eval(u, e)
	if len(values) != 1 {
		return flowLambda{}, false
	}
	lambda, ok := values[0].(flowLambda)
	return lambda, ok
}
