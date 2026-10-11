package hover

import (
	"cmp"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

const (
	sectionSeparator = "\n\n---\n\n"
	previewLines     = 6
	exampleLines     = 6
	exampleLanguage  = "bash"
	diffLanguage     = "diff"
)

var fixLabels = map[lint.Safety]string{
	lint.Safe:   "**Fix** (safe):",
	lint.Review: "**Fix** (changes behaviour, review it):",
}

func (h hoverer) withFindings(r Result, ok bool) (Result, bool) {
	findings := h.findingsAt()
	if len(findings) == 0 {
		return r, ok
	}
	sections := make([]string, 0, len(findings)+1)
	for _, f := range findings {
		sections = append(sections, h.findingDoc(f))
	}
	if !ok {
		return Result{Markdown: strings.Join(sections, sectionSeparator), Span: findings[0].Span}, true
	}
	sections = append(sections, r.Markdown)
	return Result{Markdown: strings.Join(sections, sectionSeparator), Span: r.Span}, true
}

func (h hoverer) findingsAt() []lint.Finding {
	if h.Lint == nil {
		return nil
	}
	report, enabled := h.Lint()
	if !enabled {
		return nil
	}
	var out []lint.Finding
	for _, f := range report.Findings {
		if f.Span.Covers(h.Offset) {
			out = append(out, f)
		}
	}
	slices.SortStableFunc(out, func(a, b lint.Finding) int {
		return cmp.Or(cmp.Compare(a.Span.Start, b.Span.Start), cmp.Compare(a.Code, b.Code))
	})
	return out
}

func (h hoverer) findingDoc(f lint.Finding) string {
	info, known := lint.Describe(f.Code)
	if !known {
		info = lint.CodeInfo{Code: f.Code, Severity: f.Severity}
	}
	parts := []string{codeHeader(info)}
	if len(f.Fixes) > 0 {
		parts = append(parts, h.fixPreview(f.Fixes[0]))
	}
	if info.Example != "" && strings.Count(info.Example, "\n") < exampleLines {
		parts = append(parts, "For example:\n\n"+fenced(exampleLanguage, info.Example))
	}
	parts = append(parts, "Silence it with `# shellcheck disable="+f.Code.String()+"` on the line above the command.", references(f.Code))
	return strings.Join(parts, "\n\n")
}

func (h hoverer) fixPreview(fix lint.Fix) string {
	label := fixLabels[fix.Safety] + " " + fix.Title
	before, after, ok := changedLines(h.text, fix.Edits)
	if !ok {
		return label
	}
	return label + "\n\n" + fenced(diffLanguage, lineDiff(before, after))
}

func changedLines(text string, edits []textedit.Edit) (before, after []string, ok bool) {
	if len(edits) == 0 {
		return nil, nil, false
	}
	lo, hi := edits[0].Start, edits[0].End
	for _, e := range edits[1:] {
		lo, hi = min(lo, e.Start), max(hi, e.End)
	}
	if lo < 0 || hi > len(text) {
		return nil, nil, false
	}
	start := strings.LastIndexByte(text[:lo], '\n') + 1
	end := len(text)
	if i := strings.IndexByte(text[hi:], '\n'); i >= 0 {
		end = hi + i
	}
	shifted := make([]textedit.Edit, len(edits))
	for i, e := range edits {
		shifted[i] = textedit.Edit{Start: e.Start - start, End: e.End - start, NewText: e.NewText}
	}
	before = strings.Split(text[start:end], "\n")
	after = strings.Split(textedit.Apply(text[start:end], shifted), "\n")
	if len(before) > previewLines || len(after) > previewLines {
		return nil, nil, false
	}
	return before, after, true
}

func lineDiff(before, after []string) string {
	common := 0
	for common < min(len(before), len(after)) && before[common] == after[common] {
		common++
	}
	trailing := 0
	for trailing < min(len(before), len(after))-common && before[len(before)-1-trailing] == after[len(after)-1-trailing] {
		trailing++
	}
	var sb strings.Builder
	write := func(prefix string, lines []string) {
		for _, line := range lines {
			sb.WriteString(prefix + line + "\n")
		}
	}
	write(" ", before[:common])
	write("-", before[common:len(before)-trailing])
	write("+", after[common:len(after)-trailing])
	write(" ", before[len(before)-trailing:])
	return strings.TrimSuffix(sb.String(), "\n")
}

func fenced(language, body string) string {
	longest, run := 0, 0
	for i := range len(body) {
		if body[i] != '`' {
			run = 0
			continue
		}
		run++
		longest = max(longest, run)
	}
	ticks := strings.Repeat("`", max(3, longest+1))
	return ticks + language + "\n" + body + "\n" + ticks
}
