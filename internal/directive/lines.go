package directive

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/internal/textline"
)

type Line struct {
	Start     int
	Next      int
	Directive Directive
}

func HeaderLines(text string) []Line {
	var out []Line
	for start := 0; start < len(text); {
		end, next := textline.End(text, start), textline.Next(text, start)
		line := text[start:end]
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			break
		}
		if d, ok := Parse(line); ok {
			out = append(out, Line{Start: start, Next: next, Directive: d})
		}
		start = next
	}
	return out
}

func SettingsLines(text string) []Line {
	var out []Line
	for start := 0; start < len(text); {
		end, next := textline.End(text, start), textline.Next(text, start)
		if d, ok := ParseSettings(text[start:end]); ok && d.Problem == nil {
			out = append(out, Line{Start: start, Next: next, Directive: d})
		}
		start = next
	}
	return out
}

func CommentLinesAbove(text string, lineStart int) []Line {
	var out []Line
	for lineStart > 0 {
		start := strings.LastIndexByte(text[:lineStart-1], '\n') + 1
		line := text[start : lineStart-1]
		if !strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") {
			break
		}
		if d, ok := Parse(line); ok {
			out = append(out, Line{Start: start, Next: lineStart, Directive: d})
		}
		lineStart = start
	}
	return out
}

func Render(key Key, value string) string {
	return "# " + Word + " " + key.String() + "=" + value
}

func HeaderInsert(text string, key Key, value string) textedit.Edit {
	at := 0
	if strings.HasPrefix(text, "#!") {
		at = textline.Next(text, 0)
	}
	if lines := HeaderLines(text); len(lines) > 0 {
		at = max(at, lines[len(lines)-1].Next)
	}
	return textedit.InsertLine(text, at, Render(key, value))
}
