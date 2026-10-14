package hover

import (
	"fmt"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/expansion"
	"github.com/stubbedev/shell-lsp/internal/glob"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/prose"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var roleDocs = map[glob.Role]string{
	glob.RoleFilenames:            "expands to the names of the existing files that match, in sorted order",
	glob.RoleTest:                 "matched against the whole left-hand side",
	glob.RoleCase:                 "matched against the whole `case` word",
	glob.RoleRemoveShortestPrefix: "the shortest prefix that matches is removed",
	glob.RoleRemoveLongestPrefix:  "the longest prefix that matches is removed",
	glob.RoleRemoveShortestSuffix: "the shortest suffix that matches is removed",
	glob.RoleRemoveLongestSuffix:  "the longest suffix that matches is removed",
	glob.RoleReplaceFirst:         "the first match, as long as possible, is replaced",
	glob.RoleReplaceAll:           "every match, each as long as possible, is replaced",
	glob.RoleReplacePrefix:        "a match at the start of the value is replaced",
	glob.RoleReplaceSuffix:        "a match at the end of the value is replaced",
	glob.RoleReplaceWhole:         "the value is replaced only when all of it matches",
	glob.RoleFilter:               "elements that match are removed; with the `(M)` flag only they are kept",
	glob.RoleCaseConversion:       "selects the characters whose case is changed",
	glob.RoleSubscript:            "searches the elements of the array",
	glob.RoleRegex:                "a POSIX extended regular expression, searched for anywhere in the left-hand side unless anchored with `^` or `$`",
}

func (h hoverer) patternDoc(site glob.Site) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "**Pattern**: %s.", roleDocs[site.Role])
	for _, piece := range site.Pattern.Pieces(h.text) {
		fmt.Fprintf(&sb, "\n- %s: %s", prose.Code(piece.Text), piece.Doc)
	}
	if note := h.matchingNote(site); note != "" {
		fmt.Fprintf(&sb, "\n\n%s", note)
	}
	for _, need := range site.Pattern.Unmet(h.options) {
		text := h.text[need.Span.Start:need.Span.End]
		fmt.Fprintf(&sb, "\n\n%s needs `%s`; this file never enables it, so the shell matches it literally.", prose.Code(text), need.Option.Enable(h.dialect))
	}
	for _, e := range site.Pattern.Errors {
		fmt.Fprintf(&sb, "\n\nInvalid pattern: %s.", e.Message)
	}
	return sb.String()
}

func (h hoverer) matchingNote(site glob.Site) string {
	switch site.Role {
	case glob.RoleFilenames:
		return strings.TrimSpace(h.unmatchedNote() + " " + h.caseNote(h.options.NoCaseGlob, "nocaseglob"))
	case glob.RoleTest, glob.RoleCase, glob.RoleReplaceFirst, glob.RoleReplaceAll, glob.RoleReplacePrefix, glob.RoleReplaceSuffix, glob.RoleRegex:
		if h.dialect == dialect.Bash || site.Role == glob.RoleRegex {
			return h.caseNote(h.options.NoCaseMatch, "nocasematch")
		}
	case glob.RoleRemoveShortestPrefix, glob.RoleRemoveLongestPrefix, glob.RoleRemoveShortestSuffix, glob.RoleRemoveLongestSuffix,
		glob.RoleReplaceWhole, glob.RoleFilter, glob.RoleCaseConversion, glob.RoleSubscript:
	}
	return ""
}

func (h hoverer) caseNote(set bool, option string) string {
	if !set {
		return ""
	}
	return fmt.Sprintf("This file sets `%s`, so letters match in either case.", option)
}

func (h hoverer) unmatchedNote() string {
	switch {
	case h.options.NullGlob:
		return "When nothing matches, it expands to nothing (`nullglob` is set in this file)."
	case h.options.FailGlob:
		return "When nothing matches, the command fails (`failglob` is set in this file)."
	case h.dialect == dialect.Zsh:
		return "When nothing matches, zsh stops with \"no matches found\"; the `(N)` qualifier makes it expand to nothing instead."
	}
	return "When nothing matches, the pattern is passed on as it is written."
}

func (h hoverer) pattern() (Result, bool) {
	site, ok := glob.For(h.Tree, h.options).At(h.Offset)
	if !ok || site.Exp != nil {
		return Result{}, false
	}
	doc := h.patternDoc(site)
	if outcome, ok := h.matchOutcome(site); ok {
		doc += "\n\n---\n\n" + outcome
	}
	return Result{Markdown: doc, Span: site.Span}, true
}

func (h hoverer) matchOutcome(site glob.Site) (string, bool) {
	got, err := expansion.New(h.Tree, h.options).Matches(site)
	if err != nil || len(got.Assumptions) == 0 {
		return "", false
	}
	var verdict string
	switch {
	case site.Negated && got.Holds:
		verdict = "does not match, so the test is true"
	case site.Negated:
		verdict = "matches, so the test is false"
	case got.Holds:
		verdict = "matches"
	default:
		verdict = "does not match"
	}
	text := fmt.Sprintf("**Result**: %s %s.", prose.Code(got.Subject), verdict)
	switch {
	case got.GroupsUncertain:
		text += fmt.Sprintf(" The whole match is %s; which part each group captures depends on the C library's regex engine, which glibc and BSD resolve differently here.", prose.Code(got.Groups[0]))
	case len(got.Groups) > 0:
		text += " " + h.captures(got.Groups)
	}
	return text + "\n\n" + h.given(got.Assumptions), true
}

func (h hoverer) captures(groups []string) string {
	if h.dialect == dialect.Zsh {
		return fmt.Sprintf("`$MATCH` is %s and `$match` is %s.", prose.Code(groups[0]), h.valueText(expansion.Value{Words: groups[1:], Array: true}))
	}
	return fmt.Sprintf("`BASH_REMATCH` is %s.", h.valueText(expansion.Value{Words: groups, Array: true}))
}

func (h hoverer) given(assumptions []expansion.Assumption) string {
	given := make([]string, 0, len(assumptions))
	for _, a := range assumptions {
		given = append(given, fmt.Sprintf("`%s` = %s (line %d)", a.Name, h.valueText(a.Value), a.Line))
	}
	return "Given " + strings.Join(given, ", ") + "."
}

func (h hoverer) expansionPattern(exp *syntax.ParamExp) (string, bool) {
	for _, site := range glob.For(h.Tree, h.options).Expansion(exp) {
		return h.patternDoc(site), true
	}
	return "", false
}

func (h hoverer) evaluated(exp *syntax.ParamExp, quoted bool) (string, bool) {
	if parser.IsPlainParam(exp) {
		return "", false
	}
	got, err := expansion.New(h.Tree, h.options).Expansion(exp, quoted)
	if err != nil || len(got.Assumptions) == 0 {
		return "", false
	}
	return fmt.Sprintf("**Result**: %s\n\n%s", h.valueText(got.Value), h.given(got.Assumptions)), true
}

func (h hoverer) valueText(v expansion.Value) string {
	if !v.Array {
		return prose.Code(strings.Join(v.Words, " "))
	}
	quoted := make([]string, len(v.Words))
	for i, w := range v.Words {
		q, err := syntax.Quote(w, h.dialect.Language())
		if err != nil {
			q = fmt.Sprintf("%q", w)
		}
		quoted[i] = q
	}
	return prose.Code("(" + strings.Join(quoted, " ") + ")")
}

func (h hoverer) quotedAt(i int) bool {
	if i == 0 {
		return false
	}
	_, ok := h.path[i-1].Node.(*syntax.DblQuoted)
	return ok
}
