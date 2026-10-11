package lang

import "github.com/stubbedev/shell-lsp/internal/embedded/regex"

type Excerpt struct {
	Text    string
	base    int
	offsets []int
}

func Verbatim(src string, start, end int) Excerpt {
	return Excerpt{Text: src[start:end], base: start}
}

func (e Excerpt) Span(start, end int) (int, int) {
	if e.offsets == nil {
		return e.base + start, e.base + end
	}
	return e.offsets[start], e.offsets[end]
}

type ExcerptBuilder struct {
	text    []byte
	offsets []int
	end     int
}

func NewExcerptBuilder(size int) *ExcerptBuilder {
	return &ExcerptBuilder{text: make([]byte, 0, size), offsets: make([]int, 0, size+1)}
}

func (b *ExcerptBuilder) Add(decoded string, start, end int) {
	for i := range len(decoded) {
		b.text = append(b.text, decoded[i])
		b.offsets = append(b.offsets, start)
	}
	b.end = end
}

func (b *ExcerptBuilder) Excerpt() Excerpt {
	return Excerpt{Text: string(b.text), offsets: append(b.offsets, b.end)}
}

func (r *Result) CheckRegex(e Excerpt, flavor regex.Flavor) regex.Pattern {
	pattern, err := regex.Check(e.Text, flavor)
	if err != nil {
		start, end := e.Span(err.Start, err.End)
		r.Fail(start, end, err.Message)
	}
	return pattern
}
