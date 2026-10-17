package justfile

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
)

func (f *File) Enclosing(offset int) []Span {
	spans := []Span{{Start: 0, End: len(f.Text)}}
	for _, it := range f.Items {
		span := it.Span()
		attrs := attributesOf(it)
		for _, a := range attrs {
			span.Start = min(span.Start, strings.LastIndexByte(f.Text[:a.Span.Start], '['))
			spans = append(spans, a.Span, a.Name.Span)
			for _, arg := range a.Args {
				spans = append(spans, arg.Span, arg.Key.Span)
			}
		}
		if offset < span.Start || offset > span.End {
			continue
		}
		spans = append(spans, span, it.Span())
		for _, e := range itemExprs(it) {
			Walk(e, func(x *Expr) {
				spans = append(spans, x.Span, x.Name.Span)
				if x.Str != nil {
					spans = append(spans, x.Str.Inner)
				}
			})
		}
		if it.Kind == ItemRecipe {
			r := it.Recipe
			spans = append(spans, r.Header, r.Name.Span)
			for _, p := range r.Params {
				spans = append(spans, p.Span, p.Name.Span)
			}
			for _, d := range slices.Concat(r.Deps, r.Subsequent) {
				spans = append(spans, d.Span, d.Target.Span())
			}
			if len(r.Body) > 0 {
				spans = append(spans, Span{Start: r.Body[0].Span.Start, End: r.Span.End})
			}
			for _, l := range r.Body {
				spans = append(spans, l.Span)
				for _, frag := range l.Fragments {
					spans = append(spans, frag.Span)
				}
			}
		}
	}
	return model.Nested(offset, spans)
}
