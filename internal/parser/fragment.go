package parser

import (
	"bytes"
	"errors"
	"sort"
	"sync"

	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const maxClosers = 32

func (t *Tree) ParseFragment(span Span, suffix string) (*syntax.File, error) {
	return parseClosed(t, span, suffix, func(p *syntax.Parser, src []byte) (*syntax.File, error) {
		return p.Parse(bytes.NewReader(src), "")
	})
}

func (t *Tree) ParseHeredocFragment(span Span, suffix string) (*syntax.Word, error) {
	return parseClosed(t, span, suffix, func(p *syntax.Parser, src []byte) (*syntax.Word, error) {
		return p.Document(bytes.NewReader(src))
	})
}

var fragmentParsers = sync.Pool{New: func() any { return newSyntaxParser(syntax.LangBash) }}

func parseClosed[T any](t *Tree, span Span, suffix string, parse func(*syntax.Parser, []byte) (T, error)) (T, error) {
	line := sort.SearchInts(t.lineStarts, span.Start+1)
	p, ok := fragmentParsers.Get().(*syntax.Parser)
	if !ok {
		panic("parser: the pool of fragment parsers holds something else")
	}
	defer fragmentParsers.Put(p)
	syntax.Variant(t.Lang())(p)
	syntax.StartAt(uint(span.Start), uint(line), uint(span.Start-t.lineStarts[line-1]+1))(p)
	src := make([]byte, 0, span.End-span.Start+len(suffix)+maxClosers)
	src = append(src, t.text[span.Start:span.End]...)
	if span.Start == 0 {
		blankByteOrderMark(src)
	}
	src = append(src, suffix...)
	var opened syntax.Pos
	for closers := 0; ; closers++ {
		result, err := parse(p, src)
		parseErr, ok := errors.AsType[syntax.ParseError](err)
		if !ok || !parseErr.Incomplete || parseErr.Closer == "" || parseErr.Pos == opened || closers == maxClosers {
			return result, err
		}
		opened = parseErr.Pos
		src = append(src, parseErr.Closer...)
	}
}
