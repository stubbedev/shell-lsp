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

func invalidUTF8(src string) int {
	for at, r := range src {
		if r == utf8.RuneError {
			if _, width := utf8.DecodeRuneInString(src[at:]); width == 1 {
				return at
			}
		}
	}
	return -1
}

func Format(src string) (string, error) {
	if at := invalidUTF8(src); at >= 0 {
		return "", refusal(src, at, "the file is not valid UTF-8")
	}
	file := syntax.Parse(src)
	if len(file.Errors) > file.BareOr {
		e := file.Errors[0]
		return "", refusal(file.Text, e.Span.Start, e.Message)
	}
	return render(buildDoc(file), len(src)), nil
}

func buildDoc(file *syntax.File) []item {
	c := newConverter(file)
	root, end := c.root(file)
	return fixup(rootDoc(root, end))
}
