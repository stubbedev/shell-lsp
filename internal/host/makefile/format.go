package makefile

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/scan"
	"github.com/stubbedev/shell-lsp/internal/textline"
)

const maxBlankLines = 2

var ErrUnformattable = errors.New("cannot format the Makefile")

type formatter struct {
	f        *File
	prefixes string
	defines  []Span
	define   int
	assigns  map[int]*Assignment
	rules    map[int]*Rule
	trimmed  map[int]bool
}

func Format(f *File) (string, error) {
	if len(f.Errors) > 0 {
		e := f.Errors[0]
		return "", fmt.Errorf("%w: line %d does not parse: %s", ErrUnformattable, f.line(e.Span.Start), e.Message)
	}
	if at := strings.IndexByte(f.Text, '\r'); at >= 0 {
		return "", fmt.Errorf("%w: line %d has a carriage return", ErrUnformattable, f.line(at))
	}
	fm, err := newFormatter(f)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.Grow(len(f.Text) + 1)
	blanks, started := 0, false
	lines := &reader{text: f.Text}
	scan.Forward(0, len(f.Text), func(pos int) int {
		inDefine := fm.inDefine(pos)
		end := lines.logicalEnd(pos)
		if inDefine {
			end = textline.End(f.Text, pos)
		}
		line := f.Text[pos:end]
		if !inDefine && blankText(line) && !fm.recipe(line) {
			blanks++
			return end + 1
		}
		if started {
			sb.WriteString(strings.Repeat("\n", min(blanks, maxBlankLines)))
		}
		started, blanks = true, 0
		if !inDefine {
			line = fm.line(pos, end)
		}
		sb.WriteString(line)
		if end < len(f.Text) || !strings.HasSuffix(line, "\n") && !scan.Continued(line) {
			sb.WriteByte('\n')
		}
		return end + 1
	})
	return sb.String(), nil
}

func newFormatter(f *File) (*formatter, error) {
	fm := &formatter{f: f, prefixes: string(defaultPrefix), assigns: map[int]*Assignment{}, rules: map[int]*Rule{}, trimmed: map[int]bool{}}
	for _, a := range f.Assignments {
		if a.Name.Text == ".RECIPEPREFIX" {
			value, ok := a.Value.Literal(f.Text)
			if !ok {
				return nil, fmt.Errorf("%w: line %d sets `.RECIPEPREFIX` to a computed value", ErrUnformattable, f.line(a.Span.Start))
			}
			if value = strings.TrimSpace(value); value != "" {
				fm.prefixes += value[:1]
			}
		}
		switch {
		case a.Define:
			fm.defines = append(fm.defines, a.Span)
		case len(a.Targets) == 0:
			fm.assigns[lineStart(f.Text, a.Span.Start)] = a
		}
	}
	for _, r := range f.Rules {
		fm.rules[lineStart(f.Text, r.Span.Start)] = r
	}
	for _, c := range f.Comments {
		if at := lineStart(f.Text, c.Start); spaces(f.Text[at:c.Start]) {
			fm.trimmed[at] = true
		}
	}
	for _, d := range f.Directives {
		fm.trimmed[lineStart(f.Text, d.Span.Start)] = true
	}
	for _, inc := range f.Includes {
		fm.trimmed[lineStart(f.Text, inc.Span.Start)] = true
	}
	for _, c := range f.Conditionals {
		fm.trimmed[lineStart(f.Text, c.Open.Span.Start)] = true
		for _, d := range c.Else {
			fm.trimmed[lineStart(f.Text, d.Span.Start)] = true
		}
		if c.End != nil {
			fm.trimmed[lineStart(f.Text, c.End.Span.Start)] = true
		}
	}
	return fm, nil
}

func lineStart(text string, offset int) int {
	return strings.LastIndexByte(text[:offset], '\n') + 1
}

func spaces(s string) bool { return strings.Trim(s, " \t") == "" }

func (fm *formatter) inDefine(pos int) bool {
	for fm.define < len(fm.defines) && fm.defines[fm.define].End <= pos {
		fm.define++
	}
	return fm.define < len(fm.defines) && fm.defines[fm.define].Start <= pos
}

func (fm *formatter) recipe(line string) bool {
	return line != "" && strings.IndexByte(fm.prefixes, line[0]) >= 0
}

func (fm *formatter) line(pos, end int) string {
	text := fm.f.Text[pos:end]
	switch {
	case fm.recipe(text):
		return text
	case fm.assigns[pos] != nil:
		return fm.assignment(fm.assigns[pos], pos, end)
	case fm.rules[pos] != nil:
		return fm.rule(fm.rules[pos], pos, end)
	case fm.trimmed[pos]:
		return trimLine(text)
	}
	return text
}

func trimLine(text string) string {
	if strings.ContainsRune(text, '\n') {
		return text
	}
	trimmed := trimTrailing(text)
	if scan.Continued(trimmed) {
		return text
	}
	return trimmed
}

func (fm *formatter) joined(from, to int, words []Word) (string, bool) {
	text := fm.f.Text
	parts := make([]string, 0, len(words))
	at := from
	for _, w := range words {
		if w.Span.Start < at || w.Span.End > to || !spaces(text[at:w.Span.Start]) || strings.HasSuffix(w.Text, "\\") {
			return "", false
		}
		parts = append(parts, w.Text)
		at = w.Span.End
	}
	return strings.Join(parts, " "), spaces(text[at:to])
}

func (fm *formatter) assignment(a *Assignment, pos, end int) string {
	text := fm.f.Text
	gap := text[a.Name.Span.End:a.Value.Span.Start]
	op := strings.Index(gap, a.Op)
	indent := text[pos : pos+len(text[pos:end])-len(strings.TrimLeft(text[pos:end], " \t"))]
	head, ok := fm.joined(len(indent)+pos, a.Name.Span.End, append(slices.Clone(a.Modifiers), a.Name))
	if !ok || a.Op == "" || op < 0 || !spaces(gap[:op]) || !spaces(gap[op+len(a.Op):]) {
		return text[pos:end]
	}
	head = indent + head + " " + a.Op
	value := strings.TrimLeft(text[a.Value.Span.Start:end], " \t")
	if value == "" {
		return head
	}
	return head + " " + value
}

func (fm *formatter) rule(r *Rule, pos, end int) string {
	text := fm.f.Text
	if strings.ContainsRune(text[pos:r.Header.End], '\n') {
		return text[pos:end]
	}
	start, stop := r.Colon, r.Colon+1
	if r.Grouped {
		start--
	}
	if r.DoubleColon {
		stop++
	}
	indent := text[pos : pos+len(text[pos:start])-len(strings.TrimLeft(text[pos:start], " \t"))]
	if strings.HasSuffix(strings.TrimRight(text[pos:start], " \t"), "$") || !spaceable(text[stop:end]) {
		return text[pos:end]
	}
	targets, ok := fm.joined(pos+len(indent), start, r.Targets)
	if !ok || !r.Grouped && strings.HasSuffix(targets, "&") {
		return text[pos:end]
	}
	colon := text[start:stop]
	if r.Grouped && !lineKeyword(targets) {
		colon = " " + colon
	}
	dynamic := func(w Word) bool { return w.Expr.Dynamic() }
	if slices.ContainsFunc(r.Targets, dynamic) {
		return text[pos:end]
	}
	if slices.ContainsFunc(slices.Concat(r.Patterns, r.Prereqs, r.OrderOnly), dynamic) {
		rest := strings.TrimLeft(text[stop:end], " \t")
		if rest == "" {
			return indent + targets + colon
		}
		return indent + targets + colon + " " + rest
	}
	deps, ok := fm.dependencies(r, stop)
	if !ok {
		return text[pos:end]
	}
	return indent + targets + colon + deps + fm.tail(r, end)
}

func spaceable(rest string) bool {
	if rest == "" || rest[0] == ' ' || rest[0] == '\t' {
		return true
	}
	if strings.HasPrefix(rest, "$(") || strings.HasPrefix(rest, "${") {
		return true
	}
	c := rest[0]
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || strings.IndexByte("._%/-", c) >= 0
}

func (fm *formatter) dependencies(r *Rule, from int) (string, bool) {
	text := fm.f.Text
	var sb strings.Builder
	at := from
	if len(r.Patterns) > 0 {
		last := r.Patterns[len(r.Patterns)-1].Span.End
		second := strings.IndexByte(text[last:r.Header.End], ':')
		if second < 0 {
			return "", false
		}
		second += last
		patterns, ok := fm.joined(at, second, r.Patterns)
		if !ok {
			return "", false
		}
		sb.WriteString(" " + patterns + ":")
		at = second + 1
	}
	orderOnly := r.Header.End
	if bar := strings.IndexByte(text[at:r.Header.End], '|'); bar >= 0 {
		orderOnly = at + bar
	}
	prereqs, ok := fm.joined(at, orderOnly, r.Prereqs)
	if !ok {
		return "", false
	}
	if prereqs != "" {
		sb.WriteString(" " + prereqs)
	}
	if orderOnly == r.Header.End {
		return sb.String(), true
	}
	after, ok := fm.joined(orderOnly+1, r.Header.End, r.OrderOnly)
	if !ok {
		return "", false
	}
	sb.WriteString(strings.TrimRight(" | "+after, " "))
	return sb.String(), true
}

func (fm *formatter) tail(r *Rule, end int) string {
	rest := strings.TrimLeft(fm.f.Text[r.Header.End:end], " \t")
	switch {
	case rest == "":
		return ""
	case len(r.Recipe) > 0 && r.Recipe[0].Inline:
		return " " + rest
	}
	return " " + trimLine(rest)
}
