package lint

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

const codeUnusedDirective = nativeBase + 2009

var unusedDirectivesFamily = family{
	codes: map[Code]meta{
		codeUnusedDirective: {severity: SeverityInfo, optional: "unused-directives"},
	},
}

var sourceDependentCodes = []Code{1090, 1091, 1094, 2154}

func (s *suppressions) disabledCodes() []DisabledCode {
	if len(s.items) == 0 {
		return nil
	}
	out := make([]DisabledCode, len(s.items))
	for i, item := range s.items {
		out[i] = DisabledCode{Span: item.span, Text: item.text, Suppressed: item.findings}
	}
	return out
}

func (p *pass) reportUnusedDirectives(suppressed *suppressions) {
	if !p.active(codeUnusedDirective) {
		return
	}
	items := suppressed.items
	silencedHere := map[int]bool{}
	for _, item := range items {
		if item.codes.Has(codeUnusedDirective) {
			silencedHere[item.place.directive] = true
		}
	}
	unused := make([]bool, len(items))
	for i, item := range items {
		unused[i] = item.hits == 0 && !silencedHere[item.place.directive] && p.reportableWhenUnused(item)
	}
	for i, item := range items {
		if !unused[i] {
			continue
		}
		var fixes []Fix
		if item.place.item >= 0 {
			fixes = append(fixes, p.removeDisabled(items, unused, i))
		}
		p.report(codeUnusedDirective, item.span, p.unusedMessage(item), fixes...)
	}
}

func (p *pass) reportableWhenUnused(item disabledItem) bool {
	if item.codes.Has(codeUnusedDirective) || p.directives[item.place.directive].placement != placedBeforeCommand {
		return false
	}
	if slices.ContainsFunc(sourceDependentCodes, item.codes.Has) {
		return false
	}
	code, single := item.codes.single()
	if !single {
		return true
	}
	m, known := p.catalog[code]
	if !known {
		return !code.parseFailure()
	}
	return m.optional == "" || p.settings.enabled(m.optional)
}

func (p *pass) unusedMessage(item disabledItem) string {
	name := item.text
	if code, single := item.codes.single(); single {
		name = code.String()
		if _, known := p.catalog[code]; !known {
			return name + " is not a check shell-lsp runs, so disabling it silences nothing here."
		}
	}
	return "Nothing here is silenced by disabling " + name + ", so it can be removed from the directive."
}

func (p *pass) removeDisabled(items []disabledItem, unused []bool, i int) Fix {
	at := items[i].place
	d := p.directives[at.directive]
	commentUnused := d.parsed.Problem == nil
	for _, pair := range d.parsed.Pairs {
		commentUnused = commentUnused && pair.Is(directive.Disable)
	}
	pairUnused := true
	for j, item := range items {
		if item.place.directive != at.directive {
			continue
		}
		commentUnused = commentUnused && unused[j]
		if item.place.pair == at.pair {
			pairUnused = pairUnused && unused[j]
		}
	}
	listed := directive.Items(d.parsed.Pairs[at.pair])
	switch {
	case commentUnused:
		return safeFix("Remove the unused directive", p.removeComment(d.span))
	case pairUnused || len(listed) == 1:
		pair := d.parsed.Pairs[at.pair]
		start := d.at(pair.KeyOffset)
		for start > d.span.Start && isBlankByte(p.text[start-1]) {
			start--
		}
		return safeFix("Remove the unused disable list", textedit.Edit{Start: start, End: d.at(pair.ValueEnd)})
	}
	k := at.item
	title := "Remove " + items[i].text + " from the directive"
	if k+1 < len(listed) {
		return safeFix(title, textedit.Edit{Start: d.at(listed[k].Offset), End: d.at(listed[k+1].Offset)})
	}
	return safeFix(title, textedit.Edit{Start: d.at(listed[k-1].End), End: d.at(listed[k].End)})
}

func (p *pass) removeComment(span parser.Span) textedit.Edit {
	start := lineStart(p.text, span.Start)
	if strings.TrimLeft(p.text[start:span.Start], " \t") == "" {
		end := span.End
		if end < len(p.text) && p.text[end] == '\n' {
			end++
		}
		return textedit.Edit{Start: start, End: end}
	}
	start = span.Start
	for start > 0 && isBlankByte(p.text[start-1]) {
		start--
	}
	return textedit.Edit{Start: start, End: span.End}
}
