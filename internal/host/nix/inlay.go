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

type ParameterHint struct {
	Offset int
	Name   string
}

func (d *Document) ParameterHints(within model.Span) []ParameterHint {
	var out []ParameterHint
	d.inspect(func(n syntax.Node) bool {
		if call, ok := n.(*syntax.Apply); ok {
			out = d.callHints(out, call, within)
		}
		return true
	})
	slices.SortStableFunc(out, func(a, b ParameterHint) int { return cmp.Compare(a.Offset, b.Offset) })
	return out
}

func (d *Document) callHints(out []ParameterHint, call *syntax.Apply, within model.Span) []ParameterHint {
	lambda := d.lambdaOf(call.Fn)
	for _, arg := range call.Args {
		if lambda == nil {
			return out
		}
		span := arg.Span()
		if p, ok := lambda.Param.(*syntax.IdentParam); ok && within.Start <= span.Start && span.End <= within.End && d.text(span) != p.Name.Name {
			out = append(out, ParameterHint{Offset: span.Start, Name: p.Name.Name})
		}
		lambda, _ = syntax.Unparenthesized(lambda.Body).(*syntax.Lambda)
	}
	return out
}

func (d *Document) lambdaOf(fn syntax.Expr) *syntax.Lambda {
	var value syntax.Expr
	switch fn := syntax.Unparenthesized(fn).(type) {
	case *syntax.Lambda:
		return fn
	case *syntax.Ident:
		value = d.boundValue(fn)
	case *syntax.Select:
		if fn.Default == nil {
			value = d.selected(fn.Target, fn.Path)
		}
	}
	lambda, _ := syntax.Unparenthesized(value).(*syntax.Lambda)
	return lambda
}

func (d *Document) boundValue(id *syntax.Ident) syntax.Expr {
	ref, ok := d.table.Of(id)
	if !ok || ref.Kind != scope.Definition || len(ref.Def.Bindings) != 1 {
		return nil
	}
	a, ok := ref.Def.Bindings[0].(*syntax.Assign)
	if !ok || len(a.Path) != 1 {
		return nil
	}
	return a.Value
}

func (d *Document) selected(target syntax.Expr, path syntax.AttrPath) syntax.Expr {
	value := syntax.Unparenthesized(target)
	if id, ok := value.(*syntax.Ident); ok {
		value = d.boundValue(id)
	}
	for len(path) > 0 {
		set, ok := syntax.Unparenthesized(value).(*syntax.AttrSet)
		if !ok {
			return nil
		}
		a := onlyAssign(set, path[0])
		if a == nil {
			return nil
		}
		rest := a.Path
		for len(rest) > 0 && len(path) > 0 && sameName(rest[0], path[0]) {
			rest, path = rest[1:], path[1:]
		}
		if len(rest) > 0 {
			return nil
		}
		value = a.Value
	}
	return value
}

func sameName(a, b syntax.Attr) bool {
	x, staticX := syntax.AttrName(a)
	y, staticY := syntax.AttrName(b)
	return staticX && staticY && x == y
}

func onlyAssign(set *syntax.AttrSet, attr syntax.Attr) *syntax.Assign {
	var found *syntax.Assign
	for _, b := range set.Bindings {
		switch b := b.(type) {
		case *syntax.Assign:
			if !sameName(b.Path[0], attr) {
				continue
			}
			if found != nil {
				return nil
			}
			found = b
		case *syntax.Inherit:
			if slices.ContainsFunc(b.Attrs, func(a syntax.Attr) bool { return sameName(a, attr) }) {
				return nil
			}
		}
	}
	return found
}
