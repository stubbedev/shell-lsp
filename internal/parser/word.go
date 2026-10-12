package parser

import (
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const expansionPlaceholder = "$"

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

func LiteralIs(w *syntax.Word, text string) bool {
	lit, ok := Literal(w)
	return ok && lit == text
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
				unescape(&sb, lit.Value, IsDoubleQuoteEscapable)
			}
		default:
			sb.WriteString(placeholder)
			literal = false
		}
	}
	return sb.String(), literal
}

func ArgumentTexts(args []*syntax.Word) []string {
	texts := make([]string, len(args))
	for i, arg := range args {
		texts[i], _ = Text(arg, expansionPlaceholder)
	}
	return texts
}

func IsPlainParam(pe *syntax.ParamExp) bool {
	return pe.Param != nil && !pe.Excl && !pe.Length && !pe.Width && pe.Flags == nil && pe.NestedParam == nil &&
		pe.Index == nil && len(pe.Modifiers) == 0 && pe.Slice == nil && pe.Repl == nil && pe.Names == 0 && pe.Exp == nil
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

func UnescapeLit(value string, doubleQuoted bool) string {
	if !strings.Contains(value, `\`) {
		return value
	}
	escapable := func(byte) bool { return true }
	if doubleQuoted {
		escapable = IsDoubleQuoteEscapable
	}
	var sb strings.Builder
	unescape(&sb, value, escapable)
	return sb.String()
}

func IsDoubleQuoteEscapable(c byte) bool {
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
	for i := 0; i < len(s); {
		unit, width := ansiCUnit(s[i:])
		sb.WriteString(unit)
		i += width
	}
	return sb.String()
}

func ANSICNul(s string) (start, end int, found bool) {
	for i := 0; i < len(s); {
		unit, width := ansiCUnit(s[i:])
		if unit == "\x00" {
			return i, i + width, true
		}
		i += width
	}
	return 0, 0, false
}

func ansiCUnit(s string) (string, int) {
	if s[0] != '\\' || len(s) == 1 {
		return s[:1], 1
	}
	if c, ok := ansiCEscapes[s[1]]; ok {
		return string([]byte{c}), 2
	}
	switch c := s[1]; {
	case c >= '0' && c <= '7':
		n := escapeDigits(s[1:], 3, 8)
		v, _ := strconv.ParseUint(s[1:1+n], 8, 16)
		return string([]byte{byte(v & 0xff)}), 1 + n
	case c == 'x' || c == 'u' || c == 'U':
		n := escapeDigits(s[2:], hexEscapeWidth(c), 16)
		if n == 0 {
			return s[:2], 2
		}
		v, _ := strconv.ParseUint(s[2:2+n], 16, 32)
		if c == 'x' {
			return string([]byte{byte(v & 0xff)}), 2 + n
		}
		return string(validRune(v)), 2 + n
	case c == 'c' && len(s) > 2:
		return string([]byte{s[2] & 0x1f}), 3
	}
	return s[:2], 2
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
