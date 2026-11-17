package manpage

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/prose"
)

type font uint8

const (
	roman font = iota
	bold
	italic
	boldItalic
)

const firstMarker = '\uE000'

var emphasis = [...]string{roman: "", bold: "**", italic: "*", boldItalic: "***"}

func fontNamed(name string) font {
	switch name {
	case "B", "3", "CB":
		return bold
	case "I", "2", "CI":
		return italic
	case "BI", "4":
		return boldItalic
	}
	return roman
}

func (f font) marker() rune { return firstMarker + rune(f) }

func markerFont(r rune) (font, bool) {
	for f := range font(len(emphasis)) {
		if f.marker() == r {
			return f, true
		}
	}
	return roman, false
}

func literalRune(r rune) rune {
	if _, ok := markerFont(r); ok {
		return unicode.ReplacementChar
	}
	return r
}

func withoutMarkers(s string) string {
	return strings.Map(func(r rune) rune {
		if _, ok := markerFont(r); ok {
			return -1
		}
		return r
	}, s)
}

type rich string

func (r rich) plain() string { return withoutMarkers(string(r)) }

func (r rich) markdown() string {
	escaped := prose.Escape(string(r))
	var sb strings.Builder
	sb.Grow(len(escaped))
	current, start := roman, 0
	for i, c := range escaped {
		f, ok := markerFont(c)
		if !ok {
			continue
		}
		writeEmphasised(&sb, current, escaped[start:i])
		current, start = f, i+utf8.RuneLen(c)
	}
	writeEmphasised(&sb, current, escaped[start:])
	return sb.String()
}

func writeEmphasised(sb *strings.Builder, f font, span string) {
	body := strings.TrimSpace(span)
	if f == roman || body == "" {
		sb.WriteString(span)
		return
	}
	lead := strings.Index(span, body)
	sb.WriteString(span[:lead])
	sb.WriteString(emphasis[f])
	sb.WriteString(body)
	sb.WriteString(emphasis[f])
	sb.WriteString(span[lead+len(body):])
}

type Doc struct {
	blocks []block
}

func (d Doc) Markdown() string {
	parts := make([]string, 0, len(d.blocks))
	for _, b := range d.blocks {
		if b.kind == literalBlock {
			parts = append(parts, prose.CodeBlock(b.text.plain()))
			continue
		}
		parts = append(parts, b.text.markdown())
	}
	return strings.Join(parts, "\n\n")
}
