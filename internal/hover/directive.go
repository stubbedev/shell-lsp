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

var directiveKeyDocs = map[string]string{
	directive.Disable:          "Silence the listed checks for the next command, or for the whole file when the directive comes before the first command. Takes codes such as `SC2086`, ranges such as `SC2000-SC2099`, or `all`, separated by commas.",
	directive.Enable:           "Turn on optional checks by name, or every optional check with `all`. Only honoured before the first command.",
	directive.Source:           "The file the following `source` or `.` command loads, for when its path is computed at run time. `/dev/null` skips it.",
	directive.SourcePath:       "A directory to look for sourced files in. `SCRIPTDIR` stands for the directory of the script itself.",
	directive.Shell:            "Lint the file as this shell, whatever its shebang says.",
	directive.ExternalSources:  "Whether sourced files outside the ones being checked may be followed. Only honoured in a `.shellcheckrc` file.",
	directive.ExtendedAnalysis: "Turn the checks that follow values through the script on (`true`) or off (`false`) for this file.",
}

func DirectiveKeyDoc(key string) string { return directiveKeyDocs[key] }

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
			key := parser.Span{Start: comment.Span.Start + pair.KeyOffset, End: comment.Span.Start + pair.KeyOffset + len(pair.Key)}
			if key.Covers(h.Offset) && pair.Known() {
				return Result{Markdown: "**" + pair.Key + "**\n\n" + directiveKeyDocs[pair.Key], Span: key}, true
			}
			if at < pair.ValueOffset || at > pair.ValueEnd {
				continue
			}
			for _, item := range directive.Items(pair) {
				span := parser.Span{Start: comment.Span.Start + item.Offset, End: comment.Span.Start + item.End}
				if span.Covers(h.Offset) {
					return h.directiveValue(pair.Key, item.Text, span)
				}
			}
		}
		return Result{}, false
	}
	return Result{}, false
}

func (h hoverer) directiveValue(key, value string, span parser.Span) (Result, bool) {
	switch key {
	case directive.Disable:
		return Result{Markdown: h.disabledDoc(value, span), Span: span}, value != ""
	case directive.Enable:
		return Result{Markdown: "**" + value + "**\n\n" + OptionalCheckDoc(value), Span: span}, value != ""
	}
	return Result{}, false
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
