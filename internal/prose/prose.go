package prose

import "strings"

func List(items []string, conjunction string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " " + conjunction + " " + items[len(items)-1]
}

func Code(text string) string {
	fence := "`"
	for strings.Contains(text, fence) {
		fence += "`"
	}
	if strings.HasPrefix(text, "`") || strings.HasSuffix(text, "`") {
		text = " " + text + " "
	}
	return fence + text + fence
}

func CodeBlock(text string) string {
	fence := "```"
	for strings.Contains(text, fence) {
		fence += "`"
	}
	return fence + "\n" + text + "\n" + fence
}

const (
	inlineMarkup = "\\`*_[]<>&|~"
	blockMarkup  = "#+-="
)

func Escape(text string) string {
	var sb strings.Builder
	sb.Grow(len(text) + len(text)/8)
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			sb.WriteByte('\n')
		}
		escapeLine(&sb, line)
	}
	return sb.String()
}

func escapeLine(sb *strings.Builder, line string) {
	body := strings.TrimLeft(line, " \t")
	sb.WriteString(line[:len(line)-len(body)])
	switch digits := len(body) - len(strings.TrimLeft(body, "0123456789")); {
	case body != "" && strings.IndexByte(blockMarkup, body[0]) >= 0:
		sb.WriteByte('\\')
	case digits > 0 && digits < len(body) && (body[digits] == '.' || body[digits] == ')'):
		sb.WriteString(body[:digits])
		sb.WriteByte('\\')
		body = body[digits:]
	}
	for i := range len(body) {
		if strings.IndexByte(inlineMarkup, body[i]) >= 0 {
			sb.WriteByte('\\')
		}
		sb.WriteByte(body[i])
	}
}
