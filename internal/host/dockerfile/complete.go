package dockerfile

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
)

func (f *File) scopeAt(offset int) model.Scope {
	scope := preamble
	for _, s := range f.Stages() {
		if s.From.Line.End < offset {
			scope = s.Scope
		}
	}
	return scope
}

func (f *File) instructionAt(offset int) *Instruction {
	for _, in := range f.Instructions {
		if in.Line.Start <= offset && offset <= in.Line.End {
			return in
		}
	}
	return nil
}

func inside(s Span, offset int) bool { return s.Start <= offset && offset <= s.End }

func (f *File) Expect(offset int) (model.Expect, bool) {
	in := f.instructionAt(offset)
	if in == nil {
		if f.inHeredoc(offset) || f.inComment(offset) {
			return model.Expect{}, false
		}
		start := offset
		for start > 0 && f.Text[start-1] != '\n' && (f.Text[start-1] == ' ' || f.Text[start-1] == '\t') {
			start--
		}
		if start > 0 && f.Text[start-1] != '\n' {
			return model.Expect{}, false
		}
		return model.Expect{Kinds: []model.Kind{model.KindKeyword}, Span: Span{Start: offset, End: offset}}, true
	}
	for target := in; target != nil; target = target.Inner {
		if inside(target.Keyword.Span, offset) {
			return model.Expect{Kinds: []model.Kind{model.KindKeyword}, Span: target.Keyword.Span}, true
		}
	}
	scope := f.scopeAt(offset)
	if in.Name == "FROM" {
		scope = preamble
	}
	if e, ok := f.expectReference(in, offset, scope); ok {
		return e, true
	}
	for _, fl := range in.Flags {
		if inside(Span{Start: fl.Span.Start + 2, End: fl.Name.Span.End}, offset) {
			return model.Expect{Kinds: []model.Kind{model.KindAttribute}, Span: fl.Name.Span}, true
		}
		if fl.Value != nil && fl.Name.Text == "from" && inside(fl.Value.Span, offset) {
			return model.Expect{Kinds: []model.Kind{model.KindStage}, Span: fl.Value.Span}, true
		}
	}
	if in.Name == "FROM" && len(in.Args) > 0 && inside(in.Args[0].Span, offset) && !strings.ContainsRune(in.Args[0].Text, '$') {
		return model.Expect{Kinds: []model.Kind{model.KindStage}, Span: in.Args[0].Span}, true
	}
	if offset > 1 && f.Text[offset-2:offset] == "--" && in.Form != FormShell {
		return model.Expect{Kinds: []model.Kind{model.KindAttribute}, Span: Span{Start: offset, End: offset}}, true
	}
	return model.Expect{}, false
}

func (f *File) expectReference(in *Instruction, offset int, scope model.Scope) (model.Expect, bool) {
	if in.Form == FormShell && inside(in.Command, offset) {
		return model.Expect{}, false
	}
	for i := offset - 1; i >= in.Line.Start && i >= offset-64; i-- {
		c := f.Text[i]
		if nameChar(c) {
			continue
		}
		if c == '{' && i > 0 && f.Text[i-1] == '$' || c == '$' {
			end := nameEnd(f.Text, i+1)
			return model.Expect{Kinds: []model.Kind{model.KindVariable}, Span: Span{Start: i + 1, End: max(end, offset)}, Scope: scope}, true
		}
		break
	}
	return model.Expect{}, false
}

func (f *File) inHeredoc(offset int) bool {
	for _, in := range f.Instructions {
		for _, h := range in.Heredocs {
			if h.Body.Start <= offset && offset <= h.Span.End {
				return true
			}
		}
	}
	return false
}

func (f *File) inComment(offset int) bool {
	for _, c := range f.Comments {
		if inside(c, offset) {
			return true
		}
	}
	return false
}

func (f *File) Invocation(int) (model.Invocation, bool) { return model.Invocation{}, false }

func (f *File) Enclosing(offset int) []Span {
	spans := []Span{{Start: 0, End: len(f.Text)}}
	for _, s := range f.Stages() {
		spans = append(spans, Span{Start: s.Span.Start, End: lastLineEnd(f.Text, s.Span)})
	}
	for _, in := range f.Instructions {
		spans = append(spans, in.Span, in.Line, in.Keyword.Span)
		for _, fl := range in.Flags {
			spans = append(spans, fl.Span, fl.Name.Span)
			if fl.Value != nil {
				spans = append(spans, fl.Value.Span)
			}
		}
		for _, w := range append(append([]Word{}, in.Args...), in.JSON...) {
			spans = append(spans, w.Span)
			for _, r := range f.References(w) {
				spans = append(spans, r.Span)
			}
		}
		for _, h := range in.Heredocs {
			spans = append(spans, h.Span, h.Body)
		}
	}
	return model.Nested(offset, spans)
}
