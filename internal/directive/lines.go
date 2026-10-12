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

func HeaderInsert(text string, key Key, value string) textedit.Edit {
	at := 0
	if strings.HasPrefix(text, "#!") {
		at = textline.Next(text, 0)
	}
	if lines := HeaderLines(text); len(lines) > 0 {
		at = max(at, lines[len(lines)-1].Next)
	}
	line := "# " + Word + " " + key.String() + "=" + value + "\n"
	if at == len(text) && text != "" && !strings.HasSuffix(text, "\n") {
		line = "\n" + line
	}
	return textedit.Edit{Start: at, End: at, NewText: line}
}
