package justfile

import (
	"slices"

	"github.com/stubbedev/shell-lsp/internal/host/model"
)

type frame struct {
	kind       model.Kind
	call       bool
	name       []string
	active     int
	dependency bool
	values     int
	lastEnd    int
}

func (f *File) Invocation(offset int) (model.Invocation, bool) {
	offset = min(offset, len(f.Text))
	if f.inComment(offset) {
		return model.Invocation{}, false
	}
	start := f.itemStart(offset)
	if frag, ok := f.bodyFragment(offset); ok {
		if frag.Kind != FragmentInterpolation || frag.Span.Start+2 > offset {
			return model.Invocation{}, false
		}
		start = frag.Span.Start + 2
	}
	tokens, ok := f.tokensUntil(start, offset)
	if !ok {
		return model.Invocation{}, false
	}
	var stack []*frame
	header, attributes := false, false
	var prev token
	for i, t := range tokens {
		top := func() *frame {
			if len(stack) == 0 {
				return nil
			}
			return stack[len(stack)-1]
		}
		switch {
		case punct(t, "["):
			attributes = i == 0 || attributes
			stack = append(stack, &frame{})
		case punct(t, "("):
			fr := &frame{}
			switch {
			case prev.kind == tokName && prev.span.End == t.span.Start && attributes && len(stack) == 1:
				fr.kind, fr.call, fr.name = model.KindAttribute, true, []string{prev.text}
			case prev.kind == tokName && prev.span.End == t.span.Start:
				fr.kind, fr.call, fr.name = model.KindFunction, true, []string{prev.text}
			case header && len(stack) == 0 || header && punct(prev, "*"):
				fr.kind, fr.call, fr.dependency = model.KindRecipe, true, true
			}
			stack = append(stack, fr)
		case punct(t, ")") || punct(t, "]"):
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case punct(t, ","):
			if fr := top(); fr != nil {
				fr.active++
			}
		case punct(t, ":") && len(stack) == 0:
			header = true
		case top() != nil && top().dependency && len(top().name) == 0 && t.kind == tokName:
			top().name = []string{t.text}
		case top() != nil && top().dependency && top().values == 0 && punct(t, "::"):
		case top() != nil && top().dependency && top().values == 0 && t.kind == tokName && punct(prev, "::"):
			top().name = append(top().name, t.text)
		case top() != nil && top().dependency && valueToken(t) && !operatorToken(prev):
			top().values++
			top().lastEnd = t.span.End
		}
		prev = t
	}
	for _, fr := range slices.Backward(stack) {
		if !fr.call || len(fr.name) == 0 {
			continue
		}
		active := fr.active
		if fr.dependency {
			active = fr.values
			if fr.values > 0 && (fr.lastEnd >= offset || operatorToken(prev)) {
				active--
			}
		}
		return model.Invocation{Name: fr.name[len(fr.name)-1], Qualifier: fr.name[:len(fr.name)-1], Kind: fr.kind, Active: active, Scope: f.scopeAt(offset)}, true
	}
	return model.Invocation{}, false
}

func valueToken(t token) bool {
	return t.kind == tokName || t.kind == tokString || t.kind == tokBacktick || punct(t, ")")
}

func operatorToken(t token) bool {
	return punct(t, "+") || punct(t, "/") || punct(t, "++") || punct(t, "&&") || punct(t, "||") || punct(t, "*")
}
