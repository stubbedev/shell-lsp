package hover

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

const suppressedSampleSize = 5

func OptionalCheckDoc(name string) string {
	if name == directive.All {
		return "Every optional check."
	}
	checks := lint.OptionalChecks()
	i := slices.IndexFunc(checks, func(check lint.OptionalCheck) bool { return check.Name == name })
	if i < 0 {
		return ""
	}
	check := checks[i]
	if len(check.Codes) == 0 {
		return check.Summary
	}
	codes := make([]string, len(check.Codes))
	for j, code := range check.Codes {
		codes[j] = "`" + code.String() + "`"
	}
	return "Turns on " + strings.Join(codes, ", ") + "."
}

func (h hoverer) directive() (Result, bool) {
	for _, comment := range h.Tree.Comments {
		if !comment.Span.Covers(h.Offset) {
			continue
		}
		d, ok := directive.Parse(h.text[comment.Span.Start:comment.Span.End])
		if !ok {
			return Result{}, false
		}
		at := h.Offset - comment.Span.Start
		for _, pair := range d.Pairs {
			key, known := pair.Key()
			if !known {
				continue
			}
			keySpan := parser.Span{Start: comment.Span.Start + pair.KeyOffset, End: comment.Span.Start + pair.KeyOffset + len(pair.Name)}
			if keySpan.Covers(h.Offset) {
				return Result{Markdown: "**" + key.String() + "**\n\n" + key.Doc(), Span: keySpan}, true
			}
			if !(parser.Span{Start: pair.ValueOffset, End: pair.ValueEnd}).Touches(at) {
				continue
			}
			for _, item := range directive.Items(pair) {
				span := parser.Span{Start: comment.Span.Start + item.Offset, End: comment.Span.Start + item.End}
				if span.Covers(h.Offset) {
					return h.directiveValue(key, item.Text, span)
				}
			}
		}
		return Result{}, false
	}
	return Result{}, false
}

func (h hoverer) directiveValue(key directive.Key, value string, span parser.Span) (Result, bool) {
	switch key.Value() {
	case directive.Codes:
		return Result{Markdown: h.disabledDoc(value, span), Span: span}, value != ""
	case directive.OptionalChecks:
		return Result{Markdown: "**" + value + "**\n\n" + OptionalCheckDoc(value), Span: span}, value != ""
	case directive.File, directive.Directory, directive.ShellName, directive.Boolean:
		return Result{}, false
	}
	panic(fmt.Sprintf("hover: directive key %s has no value kind", key))
}

func (h hoverer) disabledDoc(value string, span parser.Span) string {
	var sb strings.Builder
	switch code, err := lint.ParseCode(value); {
	case value == directive.All:
		sb.WriteString("**all**\n\nEvery check.")
	case err == nil:
		info, known := lint.Describe(code)
		if !known {
			return "**" + code.String() + "** is not a check shell-lsp runs.\n\n[" + referenceTitle(code) + "](" + code.Reference() + ")"
		}
		sb.WriteString(CodeDoc(info))
	default:
		sb.WriteString("**" + value + "**\n\nEvery check in this range.")
	}
	if h.Lint == nil {
		return sb.String()
	}
	if report, ok := h.Lint(); ok {
		sb.WriteString("\n\n" + h.silenced(report, span))
	}
	return sb.String()
}

func (h hoverer) silenced(report lint.Report, span parser.Span) string {
	for _, d := range report.Disabled {
		if d.Span != span {
			continue
		}
		switch n := len(d.Suppressed); n {
		case 0:
			return "Silences nothing here."
		default:
			var sb strings.Builder
			sb.WriteString("Silences " + plural(n, "finding") + " here:")
			for _, i := range d.Suppressed[:min(n, suppressedSampleSize)] {
				f := report.Suppressed[i]
				fmt.Fprintf(&sb, "\n- line %d, %s: %s", h.Tree.SourceLine(f.Span.Start), f.Code, f.Message)
			}
			if n > suppressedSampleSize {
				sb.WriteString("\n- and " + strconv.Itoa(n-suppressedSampleSize) + " more")
			}
			return sb.String()
		}
	}
	return "Silences nothing here."
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}
