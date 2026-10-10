package parser

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

func Literal(w *syntax.Word) (string, bool) {
	if w == nil {
		return "", false
	}
	var sb strings.Builder
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			unescape(&sb, p.Value, func(byte) bool { return true })
		case *syntax.SglQuoted:
			if p.Dollar {
				return "", false
			}
			sb.WriteString(p.Value)
		case *syntax.DblQuoted:
			if p.Dollar {
				return "", false
			}
			for _, inner := range p.Parts {
				lit, ok := inner.(*syntax.Lit)
				if !ok {
					return "", false
				}
				unescape(&sb, lit.Value, isDoubleQuoteEscapable)
			}
		default:
			return "", false
		}
	}
	return sb.String(), true
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
