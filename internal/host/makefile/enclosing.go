package makefile

import (
	"slices"

	"github.com/stubbedev/shell-lsp/internal/host/model"
)

func (f *File) Enclosing(offset int) []Span {
	spans := []Span{{Start: 0, End: len(f.Text)}}
	expr := func(e Expr) {
		spans = append(spans, e.Span)
		Walk(e, func(n Node) bool {
			spans = append(spans, n.Span, n.Name.Span)
			for _, a := range n.Args {
				spans = append(spans, a.Span)
			}
			return true
		})
	}
	words := func(ws []Word) {
		for _, w := range ws {
			spans = append(spans, w.Span)
			expr(w.Expr)
		}
	}
	for _, a := range f.Assignments {
		spans = append(spans, a.Span, a.Name.Span)
		words(a.Modifiers)
		words(a.Targets)
		expr(a.Value)
	}
	for _, r := range f.Rules {
		spans = append(spans, r.Span, r.Header)
		words(slices.Concat(r.Targets, r.Prereqs, r.OrderOnly, r.Patterns))
		if len(r.Recipe) > 0 {
			spans = append(spans, Span{Start: r.Recipe[0].Span.Start, End: r.Recipe[len(r.Recipe)-1].Span.End})
		}
		for _, l := range r.Recipe {
			spans = append(spans, l.Span)
			expr(l.Expr)
		}
	}
	for _, inc := range f.Includes {
		spans = append(spans, inc.Span, inc.Keyword.Span)
		words(inc.Files)
	}
	directive := func(d Directive) {
		spans = append(spans, d.Span, d.Keyword.Span)
		words(d.Names)
		expr(d.Args)
	}
	for _, d := range f.Directives {
		directive(*d)
	}
	for _, c := range f.Conditionals {
		spans = append(spans, c.Span)
		directive(c.Open)
		for _, e := range c.Else {
			directive(e)
		}
	}
	for _, e := range f.Expressions {
		expr(e)
	}
	return model.Nested(offset, spans)
}
