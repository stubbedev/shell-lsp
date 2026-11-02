package nix

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/textline"
)

type suppression struct {
	scope syntax.Span
	codes lint.CodeSet
}

func (s suppression) covers(f lint.Finding) bool {
	return s.scope.Start <= f.Span.Start && f.Span.Start <= s.scope.End && s.codes.Has(f.Code)
}

func unsuppressed(tree *syntax.File, findings []lint.Finding) []lint.Finding {
	if len(findings) == 0 {
		return findings
	}
	all := suppressions(tree)
	if len(all) == 0 {
		return findings
	}
	kept := make([]lint.Finding, 0, len(findings))
	for _, f := range findings {
		silenced := false
		for _, s := range all {
			silenced = silenced || s.covers(f)
		}
		if !silenced {
			kept = append(kept, f)
		}
	}
	return kept
}

func suppressions(tree *syntax.File) []suppression {
	var out []suppression
	var starts map[int]syntax.Span
	for i, comment := range tree.Comments {
		if comment.Kind != syntax.LineComment {
			continue
		}
		parsed, ok := directive.Parse(tree.Text[comment.Span.Start:comment.Span.End])
		if !ok {
			continue
		}
		codes, disables := disabledCodes(parsed)
		if !disables {
			continue
		}
		if starts == nil {
			starts = outermostNodes(tree)
		}
		if scope, applies := directiveScope(tree, i, starts); applies {
			out = append(out, suppression{scope: scope, codes: codes})
		}
	}
	return out
}

func disabledCodes(d directive.Directive) (lint.CodeSet, bool) {
	var codes lint.CodeSet
	disables := false
	for _, value := range d.Values(directive.Disable) {
		for item := range strings.SplitSeq(value, ",") {
			if one, err := lint.ParseCodeSet(item); err == nil {
				codes, disables = codes.Union(one), true
			}
		}
	}
	return codes, disables
}

func directiveScope(tree *syntax.File, i int, starts map[int]syntax.Span) (syntax.Span, bool) {
	text := tree.Text
	comment := tree.Comments[i].Span
	if comment.End <= tree.Root.Span().Start {
		return syntax.Span{End: len(text)}, true
	}
	lineStart := textline.Start(text, comment.Start)
	if strings.TrimSpace(text[lineStart:comment.Start]) != "" {
		return syntax.Span{Start: lineStart, End: comment.Start}, true
	}
	scope, ok := starts[nextCode(tree, i)]
	return scope, ok
}

func nextCode(tree *syntax.File, i int) int {
	at := skipBlank(tree.Text, tree.Comments[i].Span.End)
	for j := i + 1; j < len(tree.Comments) && tree.Comments[j].Span.Start == at; j++ {
		at = skipBlank(tree.Text, tree.Comments[j].Span.End)
	}
	return at
}

func outermostNodes(tree *syntax.File) map[int]syntax.Span {
	starts := map[int]syntax.Span{}
	syntax.Inspect(tree.Root, func(n syntax.Node) bool {
		span := n.Span()
		if _, known := starts[span.Start]; !known {
			starts[span.Start] = span
		}
		return true
	})
	return starts
}
