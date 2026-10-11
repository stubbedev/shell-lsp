package actions

import (
	"cmp"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const maxIndentStep = 8

type verbatimLines []parser.Span

func verbatimLinesOf(tree *parser.Tree) verbatimLines {
	text := tree.Text()
	out := make(verbatimLines, 0, len(tree.Heredocs))
	for _, h := range tree.Heredocs {
		out = append(out, heredocBody(text, h))
	}
	parser.Walk(tree.File, func(node syntax.Node) bool {
		switch node.(type) {
		case *syntax.Lit, *syntax.SglQuoted:
			span, ok := tree.NodeSpan(node)
			if !ok {
				return true
			}
			if newline := strings.IndexByte(text[span.Start:span.End], '\n'); newline >= 0 {
				out = append(out, parser.Span{Start: span.Start + newline + 1, End: span.End})
			}
		}
		return true
	})
	slices.SortFunc(out, func(a, b parser.Span) int { return cmp.Compare(a.Start, b.Start) })
	merged := out[:0]
	for _, span := range out {
		if n := len(merged); n > 0 && span.Start <= merged[n-1].End+1 {
			merged[n-1].End = max(merged[n-1].End, span.End)
			continue
		}
		merged = append(merged, span)
	}
	return merged
}

func (v verbatimLines) has(lineAt int) bool {
	i, _ := slices.BinarySearchFunc(v, lineAt, func(s parser.Span, at int) int {
		if s.Start <= at {
			return -1
		}
		return 1
	})
	return i > 0 && v[i-1].Contains(lineAt)
}

func heredocBody(text string, h parser.Heredoc) parser.Span {
	first := min(lineEnd(text, h.Span.Start)+1, len(text))
	return parser.Span{Start: first, End: lineEnd(text, max(min(h.Body.End, len(text)), first))}
}

func indentUnit(tree *parser.Tree, verbatim verbatimLines, configured uint) string {
	text := tree.Text()
	tabbed, spaced, previous := 0, 0, 0
	var steps [maxIndentStep + 1]int
	for at := 0; at < len(text); at = lineEnd(text, at) + 1 {
		lead := leadingSpace(text, at)
		if at+len(lead) == lineEnd(text, at) || verbatim.has(at) {
			continue
		}
		switch {
		case lead == "":
			previous = 0
		case lead[0] == '\t':
			tabbed++
		case strings.Trim(lead, " ") == "":
			spaced++
			if len(lead) > previous {
				steps[min(len(lead)-previous, maxIndentStep)]++
			}
			previous = len(lead)
		}
	}
	if tabbed > 0 && tabbed >= spaced {
		return "\t"
	}
	if spaced > 0 {
		best := 1
		for step := 2; step <= maxIndentStep; step++ {
			if steps[step] > steps[best] {
				best = step
			}
		}
		return strings.Repeat(" ", best)
	}
	if configured == 0 {
		return "\t"
	}
	return strings.Repeat(" ", int(configured))
}

func reindent(tree *parser.Tree, verbatim verbatimLines, span parser.Span, indent string) string {
	text := tree.Text()
	type line struct {
		text             string
		inline, verbatim bool
	}
	var lines []line
	common := -1
	for at := span.Start; at < span.End; at = lineEnd(text, at) + 1 {
		l := line{text: text[at:min(lineEnd(text, at), span.End)], inline: at != lineStart(text, at)}
		l.verbatim = !l.inline && verbatim.has(at)
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
