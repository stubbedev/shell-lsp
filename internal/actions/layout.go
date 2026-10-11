package actions

import (
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/stubbedev/shell-lsp/internal/parser"
)

func heredocLines(tree *parser.Tree) []parser.Span {
	text := tree.Text()
	out := make([]parser.Span, len(tree.Heredocs))
	for i, h := range tree.Heredocs {
		first := min(lineEnd(text, h.Span.Start)+1, len(text))
		out[i] = parser.Span{Start: first, End: lineEnd(text, max(min(h.Body.End, len(text)), first))}
	}
	return out
}

func reindent(tree *parser.Tree, span parser.Span, indent string) string {
	text := tree.Text()
	heredocs := heredocLines(tree)
	type line struct {
		text             string
		inline, verbatim bool
	}
	var lines []line
	common := -1
	for at := span.Start; at < span.End; at = lineEnd(text, at) + 1 {
		l := line{text: text[at:min(lineEnd(text, at), span.End)], inline: at != lineStart(text, at)}
		l.verbatim = !l.inline && verbatimLine(tree, heredocs, at)
		lines = append(lines, l)
		if l.inline || l.verbatim || strings.TrimSpace(l.text) == "" {
			continue
		}
		if width := len(leadingSpace(l.text, 0)); common < 0 || width < common {
			common = width
		}
	}
	var sb strings.Builder
	for _, l := range lines {
		switch {
		case l.verbatim:
			sb.WriteString(l.text)
		case l.inline:
			sb.WriteString(indent + strings.TrimLeft(l.text, " \t"))
		case strings.TrimSpace(l.text) != "":
			sb.WriteString(indent + l.text[common:])
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

func verbatimLine(tree *parser.Tree, heredocs []parser.Span, at int) bool {
	if slices.ContainsFunc(heredocs, func(s parser.Span) bool { return s.Contains(at) }) {
		return true
	}
	if at == 0 {
		return false
	}
	newline := at - 1
	for _, node := range slices.Backward(tree.PathAt(newline)) {
		span, ok := tree.NodeSpan(node)
		if !ok || !span.Covers(newline) {
			continue
		}
		switch node.(type) {
		case *syntax.Word, *syntax.Lit, *syntax.SglQuoted, *syntax.DblQuoted, *syntax.ParamExp, *syntax.ExtGlob, *syntax.BraceExp:
			return true
		}
		return false
	}
	return false
}
