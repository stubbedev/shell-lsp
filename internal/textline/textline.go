package textline

import (
	"iter"
	"strings"
)

func Start(text string, offset int) int {
	return strings.LastIndexByte(text[:offset], '\n') + 1
}

func End(text string, offset int) int {
	if i := strings.IndexByte(text[offset:], '\n'); i >= 0 {
		return offset + i
	}
	return len(text)
}

func Bounds(text string, offset int) (int, int) {
	return Start(text, offset), End(text, offset)
}

func Next(text string, offset int) int {
	return min(End(text, offset)+1, len(text))
}

func BlankBefore(text string, offset int) bool {
	return strings.TrimSpace(text[Start(text, offset):offset]) == ""
}

func BlankAfter(text string, offset int) bool {
	return strings.TrimSpace(text[offset:End(text, offset)]) == ""
}

func Owns(text string, start, end int) bool {
	return BlankBefore(text, start) && BlankAfter(text, end)
}

func CommentsAbove(text string, lineStart int) iter.Seq2[int, int] {
	return func(yield func(start, end int) bool) {
		for lineStart > 0 {
			end := lineStart - 1
			start := Start(text, end)
			if !strings.HasPrefix(strings.TrimSpace(text[start:end]), "#") || !yield(start, end) {
				return
			}
			lineStart = start
		}
	}
}

func CommentBlockStart(text string, lineStart int) int {
	for start, end := range CommentsAbove(text, lineStart) {
		if strings.HasPrefix(strings.TrimSpace(text[start:end]), "#!") {
			break
		}
		lineStart = start
	}
	return lineStart
}
