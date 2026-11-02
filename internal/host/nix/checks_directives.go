package nix

import (
	"fmt"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/shellcheckrc"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/internal/textline"
)

var (
	badDisabledCode     = directiveCode("SC1125")
	unusedDirectiveCode = directiveCode("SL2009")
)

func UnusedDirectivesCheck() string {
	info, known := lint.Describe(unusedDirectiveCode)
	if !known {
		panic("nix: " + unusedDirectiveCode.String() + " is not in the catalog")
	}
	return info.Optional
}

func directiveCode(name string) lint.Code {
	code, err := lint.ParseCode(name)
	if err != nil {
		panic("nix: " + name + " is no code: " + err.Error())
	}
	return code
}

func directiveFinding(code lint.Code, span syntax.Span, message string, fixes ...lint.Fix) lint.Finding {
	info, known := lint.Describe(code)
	if !known {
		panic("nix: " + code.String() + " is not in the catalog")
	}
	return lint.Finding{Code: code, Severity: info.Severity, Span: span, Message: message, Fixes: fixes}
}

type suppression struct {
	scope   syntax.Span
	span    syntax.Span
	text    string
	codes   lint.CodeSet
	comment int
	listed  bool
	hits    int
}

func (s suppression) covers(f lint.Finding) bool {
	return s.scope.Start <= f.Span.Start && f.Span.Start <= s.scope.End && s.codes.Has(f.Code)
}

func unsuppressed(tree *syntax.File, findings []lint.Finding) []lint.Finding {
	all := suppressions(tree)
	kept := silence(findings, all)
	kept = append(kept, badCodes(tree)...)
	if optedIn(tree) {
		kept = append(kept, unusedDirectives(tree, all)...)
	}
	return kept
}

func silence(findings []lint.Finding, all []suppression) []lint.Finding {
	kept := make([]lint.Finding, 0, len(findings))
	for _, f := range findings {
		silenced := false
		for i := range all {
			if all[i].covers(f) {
				all[i].hits++
				silenced = true
			}
		}
		if !silenced {
			kept = append(kept, f)
		}
	}
	return kept
}

func parsedDirective(tree *syntax.File, comment syntax.Comment) (directive.Directive, bool) {
	if comment.Kind != syntax.LineComment {
		return directive.Directive{}, false
	}
	return directive.Parse(tree.Text[comment.Span.Start:comment.Span.End])
}

func suppressions(tree *syntax.File) []suppression {
	var out []suppression
	var starts map[int]syntax.Span
	for i, comment := range tree.Comments {
		parsed, ok := parsedDirective(tree, comment)
		if !ok {
			continue
		}
		items := disabledItems(comment, parsed)
		if len(items) == 0 {
			continue
		}
		if starts == nil {
			starts = outermostNodes(tree)
		}
		applied, applies := directiveScope(tree, i, starts)
		if !applies {
			continue
		}
		for _, item := range items {
			item.scope, item.comment = applied, i
			out = append(out, item)
		}
	}
	return out
}

func disabledItems(comment syntax.Comment, parsed directive.Directive) []suppression {
	var out []suppression
	for _, pair := range parsed.Pairs {
		if !pair.Is(directive.Disable) {
			continue
		}
		items := directive.Items(pair)
		listed := items != nil
		if !listed {
			for text := range strings.SplitSeq(pair.Value, ",") {
				items = append(items, directive.Item{Text: text, Offset: pair.ValueOffset, End: pair.ValueEnd})
			}
		}
		for _, item := range items {
			codes, err := lint.ParseCodeSet(item.Text)
			if err != nil {
				continue
			}
			span := syntax.Span{Start: comment.Span.Start + item.Offset, End: comment.Span.Start + item.End}
			out = append(out, suppression{span: span, text: item.Text, codes: codes, listed: listed})
		}
	}
	return out
}

func badCodes(tree *syntax.File) []lint.Finding {
	var out []lint.Finding
	for _, comment := range tree.Comments {
		parsed, ok := parsedDirective(tree, comment)
		if !ok {
			continue
		}
		for _, pair := range parsed.Pairs {
			if !pair.Is(directive.Disable) || pair.Value == "" {
				continue
			}
			if item, bad := firstBadCode(pair.Value); bad {
				span := syntax.Span{Start: comment.Span.Start + pair.ValueOffset, End: comment.Span.Start + pair.ValueEnd}
				out = append(out, directiveFinding(badDisabledCode, span, fmt.Sprintf("%q is not a check code or range, so the directive does not silence it; write codes like SL8005 or ranges like SL8000-SL8099.", item)))
			}
		}
	}
	return out
}

func firstBadCode(value string) (string, bool) {
	for item := range strings.SplitSeq(value, ",") {
		if _, err := lint.ParseCodeSet(item); err != nil {
			return item, true
		}
	}
	return "", false
}

func optedIn(tree *syntax.File) bool {
	check := UnusedDirectivesCheck()
	for _, comment := range tree.Comments {
		if comment.Span.End > tree.Root.Span().Start {
			return false
		}
		parsed, ok := parsedDirective(tree, comment)
		if !ok {
			continue
		}
		for _, value := range parsed.Values(directive.Enable) {
			for name := range strings.SplitSeq(value, ",") {
				if name == check || name == directive.All {
					return true
				}
			}
		}
	}
	return false
}

func unusedDirectives(tree *syntax.File, all []suppression) []lint.Finding {
	var out []lint.Finding
	for i, item := range all {
		if item.hits > 0 || slices.ContainsFunc(all, func(other suppression) bool {
			return other.comment == item.comment && other.codes.Has(unusedDirectiveCode)
		}) {
			continue
		}
		message := "Nothing here is silenced by disabling " + item.text + ", so it can be removed from the directive."
		out = append(out, directiveFinding(unusedDirectiveCode, item.span, message, removeUnused(tree, all, i)...))
	}
	return out
}

func removeUnused(tree *syntax.File, all []suppression, i int) []lint.Fix {
	item := all[i]
	if !item.listed {
		return nil
	}
	comment := tree.Comments[item.comment]
	var same []suppression
	for _, other := range all {
		if other.comment == item.comment {
			same = append(same, other)
		}
	}
	parsed, _ := parsedDirective(tree, comment)
	allUnused := len(parsed.Pairs) == 1 && !slices.ContainsFunc(same, func(s suppression) bool { return s.hits > 0 })
	if allUnused && len(same) == strings.Count(parsed.Pairs[0].Value, ",")+1 {
		c := checker{tree: tree}
		return []lint.Fix{{Title: "Remove the unused directive", Safety: lint.Safe, Edits: []textedit.Edit{c.statementRemoval(comment.Span)}}}
	}
	j := slices.IndexFunc(same, func(s suppression) bool { return s.span == item.span })
	switch {
	case j+1 < len(same) && same[j+1].span.Start == item.span.End+1:
		return []lint.Fix{{Title: "Remove " + item.text + " from the directive", Safety: lint.Safe, Edits: []textedit.Edit{{Start: item.span.Start, End: same[j+1].span.Start}}}}
	case j > 0 && same[j-1].span.End == item.span.Start-1:
		return []lint.Fix{{Title: "Remove " + item.text + " from the directive", Safety: lint.Safe, Edits: []textedit.Edit{{Start: same[j-1].span.End, End: item.span.End}}}}
	}
	return nil
}

func UnusedDirectiveFindings(text string) []lint.Finding {
	tree := syntax.Parse(text)
	if len(tree.Errors) > 0 || optedIn(tree) {
		return nil
	}
	all := suppressions(tree)
	if len(all) == 0 {
		return nil
	}
	bare := *tree
	bare.Comments = nil
	table := scope.Resolve(tree)
	f := &file{src: text, tree: tree, table: table}
	f.discover()
	silence(check(&bare, table, f.quotingRisks()), all)
	return unusedDirectives(tree, all)
}

func (d *Document) IgnoreFixes(findings []lint.Finding) [][]lint.Fix {
	starts := outermostNodes(d.tree)
	out := make([][]lint.Fix, len(findings))
	for i, f := range findings {
		if !f.Code.Nix() || f.Code == lint.CodeNixSyntax {
			continue
		}
		if edit, ok := d.ignoreHere(f, starts); ok {
			out[i] = append(out[i], lint.Fix{Title: "Ignore " + f.Code.String() + " here", Safety: lint.Review, Edits: []textedit.Edit{edit}})
		}
		if edit, ok := shellcheckrc.HeaderDisableEdit(d.tree.Text, f.Code); ok {
			out[i] = append(out[i], lint.Fix{Title: "Ignore " + f.Code.String() + " in this file", Safety: lint.Review, Edits: []textedit.Edit{edit}})
		}
	}
	return out
}

func (d *Document) ignoreHere(f lint.Finding, starts map[int]syntax.Span) (textedit.Edit, bool) {
	text := d.tree.Text
	lineStart := textline.Start(text, f.Span.Start)
	first := skipBlank(text, lineStart)
	if node, ok := starts[first]; ok && first > d.tree.Root.Span().Start && node.Start <= f.Span.Start && f.Span.Start <= node.End && d.betweenLines(lineStart) {
		return shellcheckrc.StatementDisableEdit(text, lineStart, textline.Indent(text, lineStart), f.Code)
	}
	lineEnd := textline.End(text, f.Span.Start)
	if !d.betweenLines(lineEnd+1) || slices.ContainsFunc(d.tree.TokensIn(syntax.Span{Start: lineStart, End: lineEnd}), func(t syntax.Token) bool {
		return t.Kind == syntax.TokLineComment || t.Kind == syntax.TokBlockComment
	}) {
		return textedit.Edit{}, false
	}
	return textedit.Edit{Start: lineEnd, End: lineEnd, NewText: " " + directive.Render(directive.Disable, f.Code.String())}, true
}

func (d *Document) betweenLines(lineStart int) bool {
	if lineStart == 0 {
		return true
	}
	tokens := d.tree.TokensIn(syntax.Span{Start: lineStart - 1, End: lineStart})
	return len(tokens) == 0 || tokens[0].Kind == syntax.TokWhitespace
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
