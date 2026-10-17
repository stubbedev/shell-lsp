package makefile

import (
	"slices"

	"github.com/stubbedev/shell-lsp/internal/host/model"
)

type frame struct {
	name   string
	closer byte
	active int
	depth  int
}

func (f *File) logicalStart(offset int) int {
	start := offset
	for start > 0 {
		i := start - 1
		for i >= 0 && f.Text[i] != '\n' {
			i--
		}
		if i <= 0 || f.Text[i-1] != '\\' {
			return i + 1
		}
		start = i
	}
	return 0
}

func (f *File) Invocation(offset int) (model.Invocation, bool) {
	offset = min(offset, len(f.Text))
	if slices.ContainsFunc(f.Comments, func(c Span) bool { return c.Start < offset && offset <= c.End }) {
		return model.Invocation{}, false
	}
	var stack []*frame
	text := f.Text
	for i := f.logicalStart(offset); i < offset; i++ {
		c := text[i]
		top := func() *frame {
			if len(stack) == 0 {
				return nil
			}
			return stack[len(stack)-1]
		}
		switch {
		case c == '$' && i+1 < offset && (text[i+1] == '(' || text[i+1] == '{'):
			fr := &frame{closer: ')'}
			if text[i+1] == '{' {
				fr.closer = '}'
			}
			if end, ok := functionName(text, i+2, offset); ok {
				fr.name = text[i+2 : end]
			}
			stack = append(stack, fr)
			i++
		case c == '$':
			i++
		case top() != nil && (c == '(' || c == '{'):
			top().depth++
		case top() != nil && c == top().closer && top().depth > 0:
			top().depth--
		case top() != nil && c == top().closer:
			stack = stack[:len(stack)-1]
		case top() != nil && c == ',' && top().depth == 0:
			top().active++
		}
	}
	for _, fr := range slices.Backward(stack) {
		if fr.name != "" {
			return model.Invocation{Name: fr.name, Kind: model.KindFunction, Active: fr.active}, true
		}
	}
	return model.Invocation{}, false
}
