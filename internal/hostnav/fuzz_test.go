package hostnav

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/host/hosttest"
	"github.com/stubbedev/shell-lsp/internal/host/makefile"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/scan"
)

func checkSeeds(f *testing.F, corpus string) {
	f.Helper()
	for _, c := range hosttest.Sections(f, filepath.Join("testdata", "check", corpus)) {
		f.Add(c.Text)
	}
}

func skipLoneCarriageReturns(t *testing.T, text string) {
	t.Helper()
	if strings.Contains(strings.ReplaceAll(text, "\r\n", ""), "\r") {
		t.Skip("editors treat a lone carriage return as a line break, so the analysis reads other lines than the tool")
	}
}

func FuzzJustChecksAgreeWithJust(f *testing.F) {
	checkSeeds(f, "just.txt")
	f.Fuzz(func(t *testing.T, text string) {
		hosttest.LiveTool(t, "just", hosttest.JustVersion)
		skipLoneCarriageReturns(t, text)
		hosttest.Agree(t, docstore.NormalizeCanonical(text), hosttest.JustVerdict(t, text), findingsAlone(t, "justfile")(text))
	})
}

var (
	makeConditionals = []string{"ifdef", "ifndef", "ifeq", "ifneq"}
	computedOutcomes = []string{"empty variable name", "missing target pattern", "target pattern contains no '%'"}
)

func computedTargets(r *makefile.Rule) bool {
	globbed := slices.ContainsFunc(r.Targets, func(w makefile.Word) bool { return strings.ContainsAny(w.Text, "*?[") })
	return globbed || len(r.Targets) > 0 && r.Targets[0].Expr.Dynamic()
}

func existingTarget(r *makefile.Rule) bool {
	return slices.ContainsFunc(r.Targets, func(w makefile.Word) bool { return strings.Trim(w.Text, "./") == "" || w.Text == "-" })
}

func computedScope(a *makefile.Assignment) bool {
	return slices.ContainsFunc(a.Targets, func(w makefile.Word) bool { return w.Expr.Dynamic() })
}

func unreadableReferenceLine(text string) bool {
	return slices.ContainsFunc(makefile.Parse(text).Errors, func(e makefile.Error) bool {
		start := strings.LastIndexByte(text[:e.Span.Start], '\n') + 1
		end := strings.IndexByte(text[e.Span.Start:], '\n')
		if end < 0 {
			end = len(text) - e.Span.Start
		}
		return strings.Contains(text[start:e.Span.Start+end], "$")
	})
}

func referencesOnly(text string, line int) bool {
	return slices.ContainsFunc(makefile.Parse(text).Expressions, func(e makefile.Expr) bool {
		return lineOf(text, logicalLineStart(text, e.Span.Start))+1 == line
	})
}

func computedRule(text string, line int) bool {
	dynamic := func(w makefile.Word) bool { return w.Expr.Dynamic() }
	return slices.ContainsFunc(makefile.Parse(text).Rules, func(r *makefile.Rule) bool {
		return (line == 0 || strings.Count(text[:r.Span.Start], "\n")+1 == line) && (slices.ContainsFunc(r.Targets, dynamic) || slices.ContainsFunc(r.Patterns, dynamic) || slices.ContainsFunc(r.Prereqs, dynamic) || slices.ContainsFunc(r.OrderOnly, dynamic))
	})
}

func referenceBeforeSemicolon(text string) bool {
	for line := range strings.SplitSeq(text, "\n") {
		before, _, found := strings.Cut(line, ";")
		if found && strings.Contains(before, "$") && !strings.Contains(before, ":") {
			return true
		}
	}
	return false
}

func escapedHashRule(text string) bool {
	for line := range strings.SplitSeq(text, "\n") {
		hash := max(strings.Index(line, `\#`), strings.Index(line, `\:`))
		if hash >= 0 && strings.Contains(line[hash:], "=") && strings.Contains(line, ":") {
			return true
		}
		name, _, assigns := strings.Cut(line, "=")
		if assigns && !strings.Contains(name, ":") && strings.Contains(line, ":") && strings.ContainsAny(strings.TrimSpace(name), " \t") {
			return true
		}
	}
	return false
}

func nestedBareParentheses(text string) bool {
	for line := range strings.SplitSeq(text, "\n") {
		if !strings.Contains(line, "$(") && !strings.Contains(line, "${") {
			continue
		}
		for j := range len(line) {
			if (line[j] == '(' || line[j] == '{') && (j == 0 || line[j-1] != '$') {
				return true
			}
		}
	}
	return false
}

func FuzzMakeChecksAgreeWithMake(f *testing.F) {
	checkSeeds(f, "make.txt")
	f.Fuzz(func(t *testing.T, text string) {
		hosttest.LiveTool(t, "make", hosttest.MakeVersion)
		skipLoneCarriageReturns(t, text)
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		if !hosttest.MakeIsolated(text) {
			t.Skip("make could run a command from this input")
		}
		if slices.ContainsFunc(makeConditionals, func(word string) bool { return strings.Contains(text, word) }) {
			t.Skip("make does not read the branches a conditional skips, while the analysis checks every branch")
		}
		if strings.ContainsAny(text, "\f\v") {
			t.Skip("make treats form feeds and vertical tabs as whitespace in some places and as text in others")
		}
		if strings.ContainsRune(text, 0) {
			t.Skip("make drops and joins lines at a NUL byte, which the analysis reports as an error instead")
		}
		if strings.Contains(text, `\;`) {
			t.Skip("make reads an escaped `;` by rules the analysis only approximates: `\\;x` alone is no error, `\\;;` is")
		}
		if strings.Contains(text, `\$`) || strings.Contains(text, `$\\`) {
			t.Skip("make lets backslashes next to a `$` interact with escapes and continuations by rules the analysis only approximates")
		}
		if referenceBeforeSemicolon(text) {
			t.Skip("make expands the references before a `;` to classify a line it cannot read otherwise, which the analysis does not evaluate")
		}
		if strings.Contains(text, "export") && slices.ContainsFunc(makefile.Parse(text).Rules, func(r *makefile.Rule) bool { return len(r.Recipe) > 0 }) {
			t.Skip("make expands exported variables only when it executes a recipe, which the dry run the oracle uses never does")
		}
		if escapedHashRule(text) {
			t.Skip("make splits the targets of a rule it reads from a line with whitespace, `\\#` or `\\:` before an `=` at the `=` too, which the analysis only approximates")
		}
		if slices.ContainsFunc([]string{"foreach", "if", "or", "and", "let"}, func(name string) bool {
			return strings.Contains(text, "$("+name+" ") || strings.Contains(text, "${"+name+" ")
		}) {
			t.Skip("make expands the body of a `foreach` over nothing and the branches `if`, `or` and `and` do not take only when needed, while the analysis checks every argument")
		}
		if nestedBareParentheses(text) {
			t.Skip("make resolves bare parentheses inside a reference name by rules the analysis only approximates")
		}
		if slices.ContainsFunc(makefile.Parse(text).Rules, existingTarget) {
			t.Skip("make never runs the recipe of a target that names an existing directory such as `.`, or of the target `-`, so it never expands it")
		}
		if slices.ContainsFunc(makefile.Parse(text).Rules, computedTargets) || slices.ContainsFunc(makefile.Parse(text).Assignments, computedScope) {
			t.Skip("make ignores a rule whose computed or globbed targets expand to nothing, which depends on the environment")
		}
		if slices.ContainsFunc(makefile.Parse(text).Assignments, func(a *makefile.Assignment) bool { return a.Name.Expr.Dynamic() }) {
			t.Skip("make defines a variable under the name it computes, which the analysis does not evaluate")
		}
		verdict := hosttest.IsolatedMakeVerdict(t, text)
		if slices.Contains(computedOutcomes, verdict.Message) && strings.Contains(text, "$") {
			t.Skip("what a computed name expands to depends on the environment make runs in")
		}
		if !verdict.Accepted && computedRule(text, verdict.Line) {
			t.Skip("make sees this rule's targets and patterns only after expanding them, which the analysis does not evaluate")
		}
		if !verdict.Accepted && verdict.Message == "recipe commences before first target" && slices.ContainsFunc(makefile.Parse(text).Expressions, func(e makefile.Expr) bool { return lineOf(text, e.Span.Start)+1 < verdict.Line }) {
			t.Skip("whether a recipe line follows a rule depends on what the line of references before it expands to, which the analysis does not evaluate")
		}
		if !verdict.Accepted && unreadableReferenceLine(text) {
			t.Skip("make expands a line it cannot classify to see what it becomes, so a variable in it can fail there first")
		}
		if !verdict.Accepted && slices.ContainsFunc(makefile.Parse(text).Rules, (*makefile.Rule).Pattern) {
			t.Skip("make chains its built-in implicit rules into pattern rules, so whether it uses one depends on rules the analysis does not model")
		}
		if !verdict.Accepted && (strings.Contains(verdict.Message, "RCS/") || strings.Contains(verdict.Message, "SCCS/")) {
			t.Skip("make's built-in RCS and SCCS rules derived the name it rejects, and the analysis does not model built-in rules")
		}
		if !verdict.Accepted && referencesOnly(text, verdict.Line) {
			t.Skip("make classifies a line of references only after expanding them, which the analysis does not evaluate")
		}
		canonical := docstore.NormalizeCanonical(text)
		hosttest.Agree(t, canonical, verdict, asMakeReports(canonical, findingsAlone(t, "Makefile")(text)))
	})
}

func asMakeReports(text string, findings []lint.Finding) []lint.Finding {
	out := slices.Clone(findings)
	for i, f := range out {
		out[i].Span.Start = logicalLineStart(text, f.Span.Start)
	}
	parsed := makefile.Parse(text)
	for _, a := range parsed.Assignments {
		for i, f := range out {
			if a.Define && a.Value.Span.Start <= f.Span.Start && f.Span.Start < a.Span.End {
				out[i].Span = parser.Span{Start: a.Span.Start, End: f.Span.End}
			}
		}
	}
	for _, r := range parsed.Rules {
		for k, l := range r.Recipe {
			for i, f := range out {
				if !l.Inline && lineOf(text, f.Span.Start) == lineOf(text, l.Span.Start) {
					out[i].Span = parser.Span{Start: lineOffset(text, lineOf(text, r.Recipe[0].Span.Start)+k), End: f.Span.End}
				}
			}
		}
	}
	return out
}

func lineOf(text string, offset int) int { return strings.Count(text[:offset], "\n") }

func lineOffset(text string, line int) int {
	at := 0
	for range line {
		at += strings.IndexByte(text[at:], '\n') + 1
	}
	return at
}

func logicalLineStart(text string, offset int) int {
	start := strings.LastIndexByte(text[:offset], '\n') + 1
	for start > 0 && scan.Continued(text[strings.LastIndexByte(text[:start-1], '\n')+1:start-1]) {
		start = strings.LastIndexByte(text[:start-1], '\n') + 1
	}
	return start
}
