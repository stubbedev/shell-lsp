package hostnav

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/host/hosttest"
	"github.com/stubbedev/shell-lsp/internal/host/makefile"
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

func referencesOnly(text string, line int) bool {
	return slices.ContainsFunc(makefile.Parse(text).Expressions, func(e makefile.Expr) bool {
		return strings.Count(text[:e.Span.Start], "\n")+1 == line
	})
}

func computedRule(text string, line int) bool {
	dynamic := func(w makefile.Word) bool { return w.Expr.Dynamic() }
	return slices.ContainsFunc(makefile.Parse(text).Rules, func(r *makefile.Rule) bool {
		return strings.Count(text[:r.Span.Start], "\n")+1 == line && (slices.ContainsFunc(r.Targets, dynamic) || slices.ContainsFunc(r.Patterns, dynamic) || slices.ContainsFunc(r.Prereqs, dynamic))
	})
}

func nestedBareParentheses(text string) bool {
	for i := 0; i+1 < len(text); i++ {
		if text[i] != '$' || text[i+1] != '(' && text[i+1] != '{' {
			continue
		}
		open, closer := text[i+1], byte(')')
		if open == '{' {
			closer = '}'
		}
		for j := i + 2; j < len(text) && text[j] != closer && text[j] != '\n'; j++ {
			if text[j] == open && text[j-1] != '$' {
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
		if strings.ContainsRune(text, 0) {
			t.Skip("make drops and joins lines at a NUL byte, which the analysis reports as an error instead")
		}
		if nestedBareParentheses(text) {
			t.Skip("make resolves bare parentheses inside a reference name by rules the analysis only approximates")
		}
		if slices.ContainsFunc(makefile.Parse(text).Rules, computedTargets) {
			t.Skip("make ignores a rule whose computed or globbed targets expand to nothing, which depends on the environment")
		}
		verdict := hosttest.IsolatedMakeVerdict(t, text)
		if slices.Contains(computedOutcomes, verdict.Message) && strings.Contains(text, "$") {
			t.Skip("what a computed name expands to depends on the environment make runs in")
		}
		if !verdict.Accepted && computedRule(text, verdict.Line) {
			t.Skip("make sees this rule's targets and patterns only after expanding them, which the analysis does not evaluate")
		}
		if !verdict.Accepted && referencesOnly(text, verdict.Line) {
			t.Skip("make classifies a line of references only after expanding them, which the analysis does not evaluate")
		}
		hosttest.Agree(t, docstore.NormalizeCanonical(text), verdict, findingsAlone(t, "Makefile")(text))
	})
}
