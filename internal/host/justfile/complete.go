package justfile

import (
	"slices"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/textline"
)

var (
	expressionKinds = []model.Kind{model.KindVariable, model.KindParameter, model.KindFunction}
	recipeKinds     = []model.Kind{model.KindRecipe, model.KindAlias, model.KindModule}
	topLevelKinds   = []model.Kind{model.KindKeyword}
)

func (f *File) itemScopes() map[int]model.Scope {
	out := map[int]model.Scope{}
	for _, it := range f.Items {
		if it.Kind == ItemRecipe || it.Kind == ItemFunction {
			out[it.Span().Start] = model.Scope(len(out) + 1)
		}
	}
	return out
}

func (f *File) scopeAt(offset int) model.Scope {
	scopes := f.itemScopes()
	for _, it := range f.Items {
		if span := it.Span(); scopes[span.Start] != model.Global && span.Start <= offset && offset <= span.End {
			return scopes[span.Start]
		}
	}
	return model.Global
}

func isWordByte(c byte) bool { return isNameByte(c) || c == ':' }

func (f *File) wordAt(offset int) Span {
	start := offset
	for start > 0 && isWordByte(f.Text[start-1]) {
		start--
	}
	end := offset
	for end < len(f.Text) && isNameByte(f.Text[end]) {
		end++
	}
	return Span{Start: start, End: end}
}

func (f *File) Expect(offset int) (model.Expect, bool) {
	offset = min(offset, len(f.Text))
	word := f.wordAt(offset)
	expect := func(kinds []model.Kind) (model.Expect, bool) {
		return model.Expect{Kinds: kinds, Span: word, Scope: f.scopeAt(offset)}, true
	}
	if f.inComment(offset) {
		return model.Expect{}, false
	}
	if frag, ok := f.bodyFragment(offset); ok {
		if frag.Kind == FragmentInterpolation && frag.Span.Start+2 <= offset {
			return expect(expressionKinds)
		}
		return model.Expect{}, false
	}
	if inside, ok := f.formatSegment(offset); ok {
		if inside {
			return expect(expressionKinds)
		}
		return model.Expect{}, false
	}
	tokens, ok := f.tokensBefore(f.itemStart(offset), word.Start)
	if !ok {
		return model.Expect{}, false
	}
	kinds, ok := classify(tokens)
	if !ok {
		return model.Expect{}, false
	}
	return expect(kinds)
}

func (f *File) bodyFragment(offset int) (Fragment, bool) {
	for _, r := range f.Recipes() {
		for _, l := range r.Body {
			if l.Span.Start <= offset && offset <= l.Span.End && offset >= l.Indent.End {
				for _, frag := range l.Fragments {
					if frag.Span.Start <= offset && offset <= frag.Span.End {
						return frag, true
					}
				}
				return Fragment{}, true
			}
		}
	}
	return Fragment{}, false
}

func (f *File) formatSegment(offset int) (bool, bool) {
	for _, it := range f.Items {
		for _, root := range itemExprs(it) {
			var found, inside bool
			Walk(root, func(e *Expr) {
				if found || e.Kind != ExprString || e.Str.Prefix != 'f' || offset <= e.Str.Inner.Start || offset > e.Str.Inner.End {
					return
				}
				for _, seg := range e.Str.Segments {
					if seg.Kind == FragmentInterpolation && seg.Span.Start+2 <= offset && offset <= seg.Span.End-2 {
						found, inside = true, true
						return
					}
				}
				found = true
			})
			if found {
				return inside, true
			}
		}
	}
	return false, false
}

func (f *File) itemStart(offset int) int {
	for _, it := range f.Items {
		if span := it.Span(); span.Start <= offset && offset <= span.End {
			return span.Start
		}
	}
	start := textline.Start(f.Text, offset)
	for start > 0 && indented(f.Text[start:]) {
		start = textline.Start(f.Text, start-1)
	}
	return start
}

func indented(line string) bool { return line != "" && (line[0] == ' ' || line[0] == '\t') }

func (f *File) inComment(offset int) bool {
	return slices.ContainsFunc(f.Comments, func(c Comment) bool { return c.Span.Start < offset && offset <= c.Span.End })
}

func (f *File) tokensBefore(start, end int) (out []token, ok bool) {
	r := &reader{text: f.Text[:end], pos: start, file: &File{}}
	defer func() {
		if p := recover(); p != nil {
			if _, bailed := p.(bail); !bailed {
				panic(p)
			}
			out, ok = nil, false
		}
	}()
	for {
		t := r.next()
		switch {
		case t.kind == tokEOF:
			return out, true
		case t.kind == tokNewline:
			out = out[:0]
		case t.kind == tokString && t.str.Unclosed, t.kind == tokBacktick && t.str.Unclosed:
			return nil, false
		case t.kind == tokPunct && (t.text == "(" || t.text == "[" || t.text == "{"):
			r.depth++
		case t.kind == tokPunct && (t.text == ")" || t.text == "]" || t.text == "}"):
			r.depth = max(r.depth-1, 0)
		}
		if t.kind != tokNewline {
			out = append(out, t)
		}
	}
}

func punct(t token, text string) bool { return t.kind == tokPunct && t.text == text }

func classify(tokens []token) ([]model.Kind, bool) {
	if len(tokens) == 0 {
		return topLevelKinds, true
	}
	first := tokens[0]
	switch {
	case punct(first, "["):
		return attributeContext(tokens)
	case first.kind == tokName && first.text == "set" && len(tokens) == 1:
		return []model.Kind{model.KindSetting}, true
	case first.kind == tokName && (first.text == "import" || first.text == "mod"):
		return nil, false
	case first.kind == tokName && first.text == "alias" && slices.ContainsFunc(tokens, func(t token) bool { return punct(t, ":=") }):
		return recipeKinds, true
	}
	depth := 0
	for i, t := range tokens {
		switch {
		case punct(t, "(") || punct(t, "[") || punct(t, "{"):
			depth++
		case punct(t, ")") || punct(t, "]") || punct(t, "}"):
			depth--
		case punct(t, ":=") && depth == 0:
			return expressionKinds, true
		case punct(t, ":") && depth == 0:
			return dependencyContext(tokens[i+1:]), true
		}
	}
	if last := tokens[len(tokens)-1]; punct(last, "=") || depth > 0 {
		return expressionKinds, true
	}
	return nil, false
}

func dependencyContext(deps []token) []model.Kind {
	depth := 0
	for _, t := range deps {
		switch {
		case punct(t, "("):
			depth++
		case punct(t, ")"):
			depth--
		}
	}
	if depth > 0 && !punct(deps[len(deps)-1], "(") {
		return expressionKinds
	}
	return recipeKinds
}

func attributeContext(tokens []token) ([]model.Kind, bool) {
	depth := 0
	for _, t := range tokens {
		switch {
		case punct(t, "("):
			depth++
		case punct(t, ")"):
			depth--
		case punct(t, "]") && depth == 0:
			return nil, false
		}
	}
	last := tokens[len(tokens)-1]
	switch {
	case depth == 0 && (punct(last, "[") || punct(last, ",")):
		return []model.Kind{model.KindAttribute}, true
	case depth > 0 || punct(last, ":"):
		return expressionKinds, true
	}
	return nil, false
}

func ValidName(_ model.Kind, name string) bool {
	r := &reader{text: name, file: &File{}}
	t := r.next()
	return t.kind == tokName && t.span.End == len(name)
}
