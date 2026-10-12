package nix

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/projection"
	"github.com/stubbedev/shell-lsp/internal/scan"
)

const closing = "\n:\n)\n"

func (f *file) body(b *projection.Builder, s shellString, opener string) int {
	src := f.src
	start, end := contentBounds(src, s.tok)
	if !s.tok.indented {
		b.Region(quotedEncoder)
		b.Opaque(start, start, opener)
		f.quoted(b, start, end)
		return end
	}
	i := start
	if nl := strings.IndexByte(src[start:end], '\n'); nl >= 0 && strings.Trim(src[start:start+nl], " \t") == "" {
		i = start + nl + 1
	}
	indent := minIndent(src, i, end)
	b.Region(indentedEncoder(strings.Repeat(" ", indent)))
	b.Opaque(start, i, "")
	if s.shebang {
		next := end
		if nl := strings.IndexByte(src[i:end], '\n'); nl >= 0 {
			next = i + nl + 1
		}
		b.Opaque(i, next, "")
		i = next
	}
	b.Opaque(i, i, opener)
	f.indentedBody(b, i, end, indent)
	return end
}

func minIndent(src string, start, end int) int {
	best := -1
	scan.Forward(start, end, func(i int) int {
		text := src[i:end]
		n := len(text) - len(strings.TrimLeft(text, " "))
		switch {
		case n == len(text):
			return end
		case text[n] == '\n':
			return i + n + 1
		}
		if best < 0 || n < best {
			best = n
		}
		return nextLine(src, i+n, end)
	})
	return max(best, 0)
}

func nextLine(src string, start, end int) int {
	next := end
	scan.Forward(start, end, func(i int) int {
		rest := src[i:end]
		switch {
		case strings.HasPrefix(rest, "'''"), strings.HasPrefix(rest, "''$"):
			return i + 3
		case strings.HasPrefix(rest, "''\\"):
			return i + 4
		case strings.HasPrefix(rest, "${"):
			return antiquoteEnd(src, i+2)
		case src[i] == '\n':
			next = i + 1
			return end
		}
		return i + 1
	})
	return min(next, end)
}

func (f *file) indentedBody(b *projection.Builder, start, end, indent int) {
	src := f.src
	scan.Forward(start, end, func(i int) int {
		line := nextLine(src, i, end)
		n := len(src[i:line]) - len(strings.TrimLeft(src[i:line], " "))
		n = min(n, indent, line-i)
		b.Opaque(i, i+n, "")
		f.decodeIndented(b, i+n, line)
		return max(line, i+1)
	})
}

func (f *file) decodeIndented(b *projection.Builder, start, end int) {
	src := f.src
	run := start
	escape := func(i, n int, text string) int {
		b.Verbatim(run, i)
		b.Escape(i, i+n, text)
		run = i + n
		return run
	}
	scan.Forward(start, end, func(i int) int {
		rest := src[i:end]
		switch {
		case strings.HasPrefix(rest, "'''"):
			return escape(i, 3, "''")
		case strings.HasPrefix(rest, "''$"):
			return escape(i, 3, "$")
		case strings.HasPrefix(rest, "''\\") && len(rest) >= 4:
			return escape(i, 4, unescape(rest[3]))
		case strings.HasPrefix(rest, "${"):
			b.Verbatim(run, i)
			run = min(antiquoteEnd(src, i+2), end)
			b.Placeholder(i, run)
			return run
		case rest[0] == '$' && len(rest) > 1 && rest[1] != '\'' && rest[1] != '\n':
			return i + 2
		}
		return i + 1
	})
	b.Verbatim(run, end)
}

func (f *file) quoted(b *projection.Builder, start, end int) {
	src := f.src
	run := start
	scan.Forward(start, end, func(i int) int {
		rest := src[i:end]
		switch {
		case rest[0] == '\\' && len(rest) > 1:
			b.Verbatim(run, i)
			b.Escape(i, i+2, unescape(rest[1]))
			run = i + 2
			return run
		case strings.HasPrefix(rest, "${"):
			b.Verbatim(run, i)
			run = min(antiquoteEnd(src, i+2), end)
			b.Placeholder(i, run)
			return run
		case rest[0] == '$' && len(rest) > 1 && rest[1] != '"' && rest[1] != '\\':
			return i + 2
		}
		return i + 1
	})
	b.Verbatim(run, end)
}

func unescape(c byte) string {
	switch c {
	case 'n':
		return "\n"
	case 'r':
		return "\r"
	case 't':
		return "\t"
	}
	return string(c)
}

func opensAntiquote(host string, start, end int, text string) bool {
	return strings.HasPrefix(text, "{") && strings.HasSuffix(host[:start], "$") ||
		strings.HasSuffix(text, "$") && strings.HasPrefix(host[end:], "{")
}

func indentedEncoder(indent string) projection.Encoder {
	return func(host string, start, end int, text string) (string, bool) {
		quotes := strings.HasPrefix(text, "'") && strings.HasSuffix(host[:start], "'") ||
			strings.HasSuffix(text, "'") && strings.HasPrefix(host[end:], "'")
		if quotes || opensAntiquote(host, start, end, text) {
			return "", false
		}
		text = strings.ReplaceAll(text, "''", "'''")
		text = strings.ReplaceAll(text, "${", "''${")
		return strings.ReplaceAll(text, "\n", "\n"+indent), true
	}
}

var quotedEscapes = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "${", `\${`)

func quotedEncoder(host string, start, end int, text string) (string, bool) {
	if opensAntiquote(host, start, end, text) {
		return "", false
	}
	return quotedEscapes.Replace(text), true
}
