package clienttext

import (
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/lsp"
)

type Line struct{ Start, End int }

type Units func(r rune, size int) uint32

func UTF16(r rune, _ int) uint32 {
	if r > 0xFFFF {
		return 2
	}
	return 1
}

func Lines(text string) []Line {
	var lines []Line
	start := 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\r':
			lines = append(lines, Line{start, i})
			if i+1 < len(text) && text[i+1] == '\n' {
				i++
			}
			start = i + 1
		case '\n':
			lines = append(lines, Line{start, i})
			start = i + 1
		}
	}
	return append(lines, Line{start, len(text)})
}

func Offset(text string, lines []Line, pos lsp.Position, units Units) int {
	if int(pos.Line) >= len(lines) {
		return len(text)
	}
	line := lines[pos.Line]
	offset := line.Start
	var counted uint32
	for offset < line.End {
		r, size := utf8.DecodeRuneInString(text[offset:line.End])
		next := counted + units(r, size)
		if next > pos.Character {
			break
		}
		counted = next
		offset += size
	}
	return offset
}

func Width(text string, line Line, units Units) uint32 {
	var counted uint32
	for i := line.Start; i < line.End; {
		r, size := utf8.DecodeRuneInString(text[i:line.End])
		counted += units(r, size)
		i += size
	}
	return counted
}
