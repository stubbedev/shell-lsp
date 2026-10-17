package hostnav

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/host/hosttest"
	"github.com/stubbedev/shell-lsp/internal/host/makefile"
	"github.com/stubbedev/shell-lsp/internal/host/oracle"
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

func FuzzRealisticMakefilesAgreeWithMake(f *testing.F) {
	for _, seed := range []string{"", "\x00\x03\x01\x05", "\x03\x00\x02\x03\x01\x0b\x01", "\x09\x01\x0a\x00\x04\x0b\x06"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, choices []byte) {
		hosttest.LiveTool(t, "make", hosttest.MakeVersion)
		text := hosttest.GenerateMakefile(choices)
		if strings.Contains(text, "export") && slices.ContainsFunc(makefile.Parse(text).Rules, func(r *makefile.Rule) bool { return len(r.Recipe) > 0 }) {
			t.Skip("make expands exported variables only when it executes a recipe, which the dry run the oracle uses never does")
		}
		verdict := hosttest.IsolatedMakeVerdict(t, text)
		hosttest.Agree(t, text, verdict, makeReports(text, verdict, findingsAlone(t, "Makefile")(text)))
	})
}

func makeReports(text string, verdict oracle.Verdict, findings []lint.Finding) []lint.Finding {
	return atExpansion(text, verdict, atDefinition(text, verdict, asMakeReports(text, findings)))
}

func atDefinition(text string, verdict oracle.Verdict, findings []lint.Finding) []lint.Finding {
	if verdict.Message != "unterminated variable reference" || verdict.Line == 0 {
		return findings
	}
	out := slices.Clone(findings)
	parsed := makefile.Parse(text)
	for i, f := range out {
		_, name, deferred := strings.Cut(f.Message, "; make fails when it expands `")
		if f.Code != lint.CodeMakeUnterminatedReference || !deferred {
			continue
		}
		name = strings.TrimSuffix(name, "`")
		if slices.ContainsFunc(parsed.Assignments, func(a *makefile.Assignment) bool {
			return a.Name.Text == name && lineOf(text, a.Span.Start)+1 == verdict.Line
		}) {
			out[i].Span = parser.Span{Start: lineOffset(text, verdict.Line-1), End: lineOffset(text, verdict.Line-1)}
		}
	}
	return out
}

func atExpansion(text string, verdict oracle.Verdict, findings []lint.Finding) []lint.Finding {
	name, recursive := strings.CutPrefix(verdict.Message, "Recursive variable '")
	name, _, _ = strings.Cut(name, "'")
	if !recursive || verdict.Line == 0 {
		return findings
	}
	out := slices.Clone(findings)
	for i, f := range out {
		if f.Code == lint.CodeMakeRecursiveVariable && strings.Contains(f.Message, "`"+name+"`") {
			out[i].Span = parser.Span{Start: lineOffset(text, verdict.Line-1), End: lineOffset(text, verdict.Line-1)}
		}
	}
	return out
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
