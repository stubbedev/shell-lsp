package format

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

type RefusalError struct {
	Line    int
	Column  int
	Message string
}

func (e *RefusalError) Error() string {
	return fmt.Sprintf("cannot format a Nix file at %d:%d: %s", e.Line, e.Column, e.Message)
}

func refusal(src string, offset int, message string) *RefusalError {
	before := src[:offset]
	start := strings.LastIndexByte(before, '\n') + 1
	return &RefusalError{Line: strings.Count(before, "\n") + 1, Column: utf8.RuneCountInString(before[start:]) + 1, Message: message}
}

func Format(src string) (string, error) {
	file := syntax.Parse(src)
	if len(file.Errors) > 0 {
		e := file.Errors[0]
		return "", refusal(file.Text, e.Span.Start, e.Message)
	}
	c := &converter{file: file}
	inner := c.expr(file.Root)
	root := c.assemble(&node{kind: kindRoot, span: syntax.Span{End: len(file.Text)}, kids: []*node{inner}}, []*node{inner})
	b := &builder{src: file.Text, used: make(map[int]bool, len(file.Comments))}
	doc := b.root(root)
	for _, comment := range file.Comments {
		if !b.used[comment.Span.Start] {
			return "", refusal(file.Text, comment.Span.Start, "a comment in this position is not supported by the formatter, and dropping it is not an option")
		}
	}
	return render(doc), nil
}
