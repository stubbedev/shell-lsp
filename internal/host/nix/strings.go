package nix

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/projection"
)

const closing = "\n:\n)\n"

func (f *file) body(b *projection.Builder, s shellString, opener string) int {
	src := f.src
	str := s.str
	start, end := str.Content.Start, str.Content.End
	if str.Kind == syntax.Quoted {
		b.Region(quotedEncoder)
		b.Opaque(start, start, opener)
		decode(b, str.Parts, start, end)
		return end
	}
	i := start
	if nl := strings.IndexByte(src[start:end], '\n'); nl >= 0 && strings.Trim(src[start:start+nl], " \t") == "" {
		i = start + nl + 1
	}
	lines := lineEnds(str)
	indent := minIndent(src, lines, i, end)
	b.Region(indentedEncoder(strings.Repeat(" ", indent)))
	b.Opaque(start, i, "")
	if s.shebang {
		next := end
		for _, stop := range lines {
			if stop > i {
				next = stop
				break
			}
		}
		b.Opaque(i, next, "")
		i = next
	}
	b.Opaque(i, i, opener)
	indentedBody(b, src, str.Parts, lines, i, indent)
	return end
}

func lineEnds(str *syntax.String) []int {
	var out []int
	for _, part := range str.Parts {
		literal, ok := part.(*syntax.Literal)
		if !ok {
			continue
		}
		at := literal.Span().Start
		for {
			nl := strings.IndexByte(literal.Text[at-literal.Span().Start:], '\n')
			if nl < 0 {
				break
			}
			at += nl + 1
			out = append(out, at)
		}
	}
	if n := len(out); n == 0 || out[n-1] != str.Content.End {
		out = append(out, str.Content.End)
	}
	return out
}

func leadingSpaces(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

func minIndent(src string, lines []int, start, end int) int {
	best := -1
	at := start
	for _, stop := range lines {
		if stop <= at {
			continue
		}
		text := src[at:end]
		n := leadingSpaces(text)
		if n == len(text) {
			break
		}
		if text[n] != '\n' && (best < 0 || n < best) {
			best = n
		}
		at = stop
	}
	return max(best, 0)
}

func indentedBody(b *projection.Builder, src string, parts []syntax.Part, lines []int, start, indent int) {
	at := start
	for _, stop := range lines {
		if stop <= at {
			continue
		}
		skipped := min(leadingSpaces(src[at:stop]), indent)
		b.Opaque(at, at+skipped, "")
		parts = decode(b, parts, at+skipped, stop)
		at = stop
	}
}

func decode(b *projection.Builder, parts []syntax.Part, start, end int) []syntax.Part {
	for len(parts) > 0 {
		s := parts[0].Span()
		if s.End <= start {
			parts = parts[1:]
			continue
		}
		if s.Start >= end {
			return parts
		}
		switch part := parts[0].(type) {
		case *syntax.Literal:
			b.Verbatim(max(s.Start, start), min(s.End, end))
		case *syntax.Escape:
			b.Escape(s.Start, s.End, part.Text)
		case *syntax.Interpolation:
			b.Placeholder(s.Start, s.End)
		}
		if s.End > end {
			return parts
		}
		parts = parts[1:]
	}
	return parts
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
