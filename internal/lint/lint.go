package lint

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

type Severity uint8

const (
	SeverityError Severity = iota + 1
	SeverityWarning
	SeverityInfo
	SeverityStyle
)

var severityNames = nameTable[Severity]{SeverityError: "error", SeverityWarning: "warning", SeverityInfo: "info", SeverityStyle: "style"}

func (s Severity) String() string { return severityNames.name(s) }

func SeverityNamed(name string) (Severity, bool) { return severityNames.named(name) }

func SeverityNames() []string { return slices.Clone(severityNames) }

type Tag uint8

const (
	TagNone Tag = iota
	TagUnnecessary
	TagDeprecated
)

type Code uint16

type codeFamily struct {
	prefix string
	base   int
	end    int
	wiki   Wiki
}

type Wiki struct {
	Name string
	URL  string
}

const (
	nativeBase   Code = 10000
	hadolintBase Code = 20000
	familyEnd    Code = 30000
)

var (
	shellcheckCodes = codeFamily{prefix: "SC", base: 0, end: int(nativeBase), wiki: Wiki{Name: "ShellCheck wiki", URL: "https://www.shellcheck.net/wiki/"}}
	nativeCodes     = codeFamily{prefix: "SL", base: int(nativeBase), end: int(hadolintBase)}
	hadolintCodes   = codeFamily{prefix: "DL", base: int(hadolintBase), end: int(familyEnd), wiki: Wiki{Name: "Hadolint wiki", URL: "https://github.com/hadolint/hadolint/wiki/"}}
	codeFamilies    = []codeFamily{shellcheckCodes, nativeCodes, hadolintCodes}
)

func (f codeFamily) has(c Code) bool { return int(c) > f.base && int(c) < f.end }

func (c Code) family() codeFamily {
	for _, f := range codeFamilies {
		if f.has(c) {
			return f
		}
	}
	panic("lint: code " + strconv.Itoa(int(c)) + " belongs to no code family")
}

func (c Code) ShellCheck() bool { return shellcheckCodes.has(c) }

func (c Code) Native() bool { return nativeCodes.has(c) }

func (c Code) Hadolint() bool { return hadolintCodes.has(c) }

func (c Code) Portability() bool { return c >= 3000 && c < 4000 }

func (c Code) parseFailure() bool { return c >= 1000 && c < 2000 }

func (c Code) String() string {
	f := c.family()
	return f.prefix + strconv.Itoa(int(c)-f.base)
}

func ParseCode(s string) (Code, error) {
	upper := strings.ToUpper(strings.TrimSpace(s))
	family := shellcheckCodes
	for _, f := range codeFamilies {
		if strings.HasPrefix(upper, f.prefix) {
			family = f
		}
	}
	n, err := strconv.ParseUint(strings.TrimPrefix(upper, family.prefix), 10, 16)
	if err != nil {
		return 0, fmt.Errorf("expected a check code like SC2034, SL1001 or DL3008, got %q: %w", s, err)
	}
	if n == 0 || int(n) >= family.end-family.base {
		return 0, fmt.Errorf("expected a check code like SC2034, SL1001 or DL3008, got %q: the number after %s must be between 1 and %d", s, family.prefix, family.end-family.base-1)
	}
	return Code(family.base + int(n)), nil //nolint:gosec // G115: base + n stays below the family's end, which a Code holds
}

type Finding struct {
	Code     Code
	Severity Severity
	Tag      Tag
	Span     parser.Span
	Message  string
	Fixes    []Fix
}

type Safety uint8

const (
	Safe Safety = iota + 1
	Review
)

type Fix struct {
	Title  string
	Safety Safety
	Edits  []textedit.Edit
}

const snippetLimit = 40

func snippet(text string) string {
	line, _, multiline := strings.Cut(text, "\n")
	if !multiline && utf8.RuneCountInString(line) <= snippetLimit {
		return line
	}
	runes := []rune(line)
	return string(runes[:min(len(runes), snippetLimit)]) + "…"
}

func safeFix(title string, edits ...textedit.Edit) Fix {
	return Fix{Title: title, Safety: Safe, Edits: edits}
}

func reviewFix(title string, edits ...textedit.Edit) Fix {
	return Fix{Title: title, Safety: Review, Edits: edits}
}

func AutoFix(fixes []Fix) (Fix, bool) {
	i := slices.IndexFunc(fixes, func(f Fix) bool { return f.Safety == Safe })
	if i < 0 {
		return Fix{}, false
	}
	return fixes[i], true
}

func SafeEdits(fixSets [][]Fix) []textedit.Edit {
	chosen := make([]Fix, 0, len(fixSets))
	for _, fixes := range fixSets {
		if fix, ok := AutoFix(fixes); ok {
			chosen = append(chosen, fix)
		}
	}
	return MergedEdits(chosen)
}

func FindingsSafeEdits(findings []Finding) []textedit.Edit {
	fixSets := make([][]Fix, len(findings))
	for i, f := range findings {
		fixSets[i] = f.Fixes
	}
	return SafeEdits(fixSets)
}

func MergedEdits(fixes []Fix) []textedit.Edit {
	groups := make([][]textedit.Edit, len(fixes))
	for i, fix := range fixes {
		groups[i] = fix.Edits
	}
	return textedit.NonOverlapping(groups)
}

type Source struct {
	Command parser.Span
	Tree    *parser.Tree
}

type DialectSource uint8

const (
	DialectDetected DialectSource = iota
	DialectForced
	DialectGuessed
)

type Normalized struct {
	ByteOrderMark   bool
	CarriageReturns []int
}

type Options struct {
	Exclude       CodeSet
	Optional      []string
	DialectSource DialectSource
	Sources       []Source
	RCFile        bool
	Profile       Profile

	Normalized Normalized

	NoExtendedAnalysis bool
}

type Report struct {
	Findings   []Finding
	Suppressed []Finding
	Disabled   []DisabledCode
}

type DisabledCode struct {
	Span       parser.Span
	Text       string
	Suppressed []int
}

func Check(tree *parser.Tree, imports []parser.Import, opts Options) []Finding {
	return Run(tree, imports, opts).Findings
}

func Run(tree *parser.Tree, imports []parser.Import, opts Options) Report {
	p := newPass(tree, imports, opts)
	name, foreign := foreignInterpreter(p.text, p.shellChosen)
	if foreign {
		first, _, _ := strings.Cut(p.text, "\n")
		p.report(1071, parser.Span{Start: 0, End: len(first)}, name+" is not a shell, so this file is not linted.")
	} else {
		p.run()
	}
	sortFindings(p.findings)
	found := slices.CompactFunc(p.findings, sameFinding)
	suppressed := p.suppressions()
	for _, f := range p.probes {
		suppressed.cover(f, -1)
	}
	report := Report{Findings: found[:0]}
	keep := func(f Finding) {
		if len(suppressed.cover(f, len(report.Suppressed))) > 0 {
			report.Suppressed = append(report.Suppressed, f)
			return
		}
		if !opts.Exclude.Has(f.Code) && p.profileKeeps(f) {
			report.Findings = append(report.Findings, f)
		}
	}
	for _, f := range found {
		keep(f)
	}
	if !foreign {
		p.findings = nil
		p.reportUnusedDirectives(suppressed)
		for _, f := range p.findings {
			keep(f)
		}
		sortFindings(report.Findings)
	}
	report.Disabled = suppressed.disabledCodes()
	return report
}

func sortFindings(findings []Finding) {
	slices.SortStableFunc(findings, func(a, b Finding) int {
		return cmp.Or(cmp.Compare(a.Span.Start, b.Span.Start), cmp.Compare(a.Code, b.Code), cmp.Compare(a.Span.End, b.Span.End))
	})
}

func sameFinding(a, b Finding) bool { return a.Code == b.Code && a.Span == b.Span }

var foreignFamily = family{codes: map[Code]meta{1071: {severity: SeverityError, summary: "A file run by an interpreter that is not a shell is not linted."}}}

var foreignInterpreters = []string{"awk", "csh", "expect", "fish", "perl", "python", "ruby", "tcsh"}

func Foreign(tree *parser.Tree, source DialectSource) bool {
	_, foreign := foreignInterpreter(tree.Text(), shellChosen(source, namesShell(parseDirectives(tree))))
	return foreign
}

func shellChosen(source DialectSource, namesShell bool) bool {
	return source == DialectForced || namesShell
}

func foreignInterpreter(text string, shellChosen bool) (string, bool) {
	name, ok := dialect.Interpreter(text)
	if !ok || shellChosen {
		return "", false
	}
	return name, slices.ContainsFunc(foreignInterpreters, func(prefix string) bool { return strings.HasPrefix(name, prefix) })
}
