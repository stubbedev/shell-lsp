package parser

import (
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"mvdan.cc/sh/v3/syntax"
)

func Literal(w *syntax.Word) (string, bool) {
	if w == nil || slices.ContainsFunc(w.Parts, isExpandedOrDollarQuoted) {
		return "", false
	}
	if len(w.Parts) == 1 {
		if lit, ok := w.Parts[0].(*syntax.Lit); ok && !strings.Contains(lit.Value, `\`) {
			return lit.Value, true
		}
	}
	text, _ := Text(w, "")
	return text, true
}

func Text(w *syntax.Word, placeholder string) (string, bool) {
	if w == nil {
		return "", false
	}
	var sb strings.Builder
	literal := true
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			unescape(&sb, p.Value, func(byte) bool { return true })
		case *syntax.SglQuoted:
			if p.Dollar {
				sb.WriteString(decodeANSIC(p.Value))
				continue
			}
			sb.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, inner := range p.Parts {
				lit, ok := inner.(*syntax.Lit)
				if !ok {
					sb.WriteString(placeholder)
					literal = false
					continue
				}
				unescape(&sb, lit.Value, isDoubleQuoteEscapable)
			}
		default:
			sb.WriteString(placeholder)
			literal = false
		}
	}
	return sb.String(), literal
}

func isExpandedOrDollarQuoted(part syntax.WordPart) bool {
	switch p := part.(type) {
	case *syntax.Lit:
		return false
	case *syntax.SglQuoted:
		return p.Dollar
	case *syntax.DblQuoted:
		return p.Dollar || slices.ContainsFunc(p.Parts, func(inner syntax.WordPart) bool {
			_, lit := inner.(*syntax.Lit)
			return !lit
		})
	}
	return true
}

func LiteralChars(w *syntax.Word) string {
	text, _ := Text(w, "")
	return text
}

func isDoubleQuoteEscapable(c byte) bool {
	return c == '$' || c == '`' || c == '"' || c == '\\' || c == '\n'
}

func unescape(sb *strings.Builder, s string, escapable func(byte) bool) {
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) || !escapable(s[i+1]) {
			sb.WriteByte(s[i])
			continue
		}
		i++
		if s[i] != '\n' {
			sb.WriteByte(s[i])
		}
	}
}

var ansiCEscapes = map[byte]byte{
	'a': '\a', 'b': '\b', 'e': 0x1b, 'E': 0x1b, 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t', 'v': '\v',
	'\\': '\\', '\'': '\'', '"': '"', '?': '?',
}

func decodeANSIC(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			sb.WriteByte(s[i])
			continue
		}
		i++
		if c, ok := ansiCEscapes[s[i]]; ok {
			sb.WriteByte(c)
			continue
		}
		switch c := s[i]; {
		case c >= '0' && c <= '7':
			n := escapeDigits(s[i:], 3, 8)
			v, _ := strconv.ParseUint(s[i:i+n], 8, 16)
			sb.WriteByte(byte(v & 0xff))
			i += n - 1
		case c == 'x' || c == 'u' || c == 'U':
			n := escapeDigits(s[i+1:], hexEscapeWidth(c), 16)
			if n == 0 {
				sb.WriteString(s[i-1 : i+1])
				continue
			}
			v, _ := strconv.ParseUint(s[i+1:i+1+n], 16, 32)
			if c == 'x' {
				sb.WriteByte(byte(v & 0xff))
			} else {
				sb.WriteRune(validRune(v))
			}
			i += n
		case c == 'c' && i+1 < len(s):
			i++
			sb.WriteByte(s[i] & 0x1f)
		default:
			sb.WriteString(s[i-1 : i+1])
		}
	}
	return sb.String()
}

func hexEscapeWidth(c byte) int {
	switch c {
	case 'u':
		return 4
	case 'U':
		return 8
	}
	return 2
}

func escapeDigits(s string, limit, base int) int {
	n := 0
	for n < limit && n < len(s) {
		if _, err := strconv.ParseUint(s[n:n+1], base, 8); err != nil {
			break
		}
		n++
	}
	return n
}

func validRune(v uint64) rune {
	if v > utf8.MaxRune || !utf8.ValidRune(rune(v)) {
		return utf8.RuneError
	}
	return rune(v)
}
