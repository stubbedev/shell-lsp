package cli

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/actions"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/format"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

type level struct {
	gcc        string
	checkstyle string
	sarif      string
	color      string
}

var levels = [...]level{
	lint.SeverityError:   {gcc: "error", checkstyle: "error", sarif: "error", color: "\x1b[31m"},
	lint.SeverityWarning: {gcc: "warning", checkstyle: "warning", sarif: "warning", color: "\x1b[33m"},
	lint.SeverityInfo:    {gcc: "note", checkstyle: "info", sarif: "note", color: "\x1b[32m"},
	lint.SeverityStyle:   {gcc: "note", checkstyle: "info", sarif: "note", color: "\x1b[32m"},
}

const (
	ansiBold  = "\x1b[1m"
	ansiReset = "\x1b[0m"
	tabStop   = 8
)

type fileReport struct {
	name     string
	doc      *docstore.Document
	findings []lint.Finding
}

type reportOptions struct {
	color     bool
	wikiLinks int
	version   string
}

type outputFormat struct {
	name      string
	write     func(out *bytes.Buffer, reports []fileReport, opts reportOptions)
	emptyNote string
}

var outputFormats = []outputFormat{
	{name: "tty", write: writeTTY},
	{name: "gcc", write: writeGCC},
	{name: "json", write: writeJSON},
	{name: "json1", write: writeJSON1},
	{name: "checkstyle", write: writeCheckstyle},
	{name: "sarif", write: writeSARIF},
	{name: "diff", write: writeDiff, emptyNote: "Issues were detected, but none were auto-fixable. Use another format to see them."},
	{name: "quiet", write: func(*bytes.Buffer, []fileReport, reportOptions) {}},
}

type place struct {
	line, column int
}

func placeOf(doc *docstore.Document, offset int) place {
	p := doc.OffsetToPosition(offset, docstore.EncodingUTF32)
	return place{line: int(p.Line) + 1, column: int(p.Character) + 1}
}

func writeGCC(out *bytes.Buffer, reports []fileReport, _ reportOptions) {
	for _, r := range reports {
		for _, f := range r.findings {
			at := placeOf(r.doc, f.Span.Start)
			fmt.Fprintf(out, "%s:%d:%d: %s: %s [%s]\n", r.name, at.line, at.column, levels[f.Severity].gcc, f.Message, f.Code)
		}
	}
}

type jsonReplacement struct {
	Column         int    `json:"column"`
	EndColumn      int    `json:"endColumn"`
	EndLine        int    `json:"endLine"`
	InsertionPoint string `json:"insertionPoint"`
	Line           int    `json:"line"`
	Precedence     int    `json:"precedence"`
	Replacement    string `json:"replacement"`
}

type jsonFix struct {
	Replacements []jsonReplacement `json:"replacements"`
}

type jsonComment struct {
	File      string   `json:"file"`
	Line      int      `json:"line"`
	EndLine   int      `json:"endLine"`
	Column    int      `json:"column"`
	EndColumn int      `json:"endColumn"`
	Level     string   `json:"level"`
	Code      int      `json:"code"`
	Message   string   `json:"message"`
	Fix       *jsonFix `json:"fix"`
}

const fixPrecedence = 1

func jsonComments(reports []fileReport) []jsonComment {
	out := []jsonComment{}
	for _, r := range reports {
		for _, f := range r.findings {
			start, end := placeOf(r.doc, f.Span.Start), placeOf(r.doc, f.Span.End)
			c := jsonComment{
				File: r.name, Line: start.line, EndLine: end.line, Column: start.column, EndColumn: end.column,
				Level: f.Severity.String(), Code: int(f.Code), Message: f.Message,
			}
			if fix, ok := lint.AutoFix(f.Fixes); ok {
				c.Fix = &jsonFix{Replacements: make([]jsonReplacement, len(fix.Edits))}
				for i, e := range fix.Edits {
					from, to := placeOf(r.doc, e.Start), placeOf(r.doc, e.End)
					point := "beforeStart"
					if e.Start == f.Span.Start {
						point = "afterEnd"
					}
					c.Fix.Replacements[i] = jsonReplacement{
						Column: from.column, EndColumn: to.column, EndLine: to.line, InsertionPoint: point,
						Line: from.line, Precedence: fixPrecedence, Replacement: e.NewText,
					}
				}
			}
			out = append(out, c)
		}
	}
	return out
}

func encodeJSON(out *bytes.Buffer, v any) {
	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		panic(fmt.Sprintf("cli: encoding a report of plain values failed: %v", err))
	}
}

func writeJSON(out *bytes.Buffer, reports []fileReport, _ reportOptions) {
	encodeJSON(out, jsonComments(reports))
}

func writeJSON1(out *bytes.Buffer, reports []fileReport, _ reportOptions) {
	encodeJSON(out, struct {
		Comments []jsonComment `json:"comments"`
	}{jsonComments(reports)})
}

func writeCheckstyle(out *bytes.Buffer, reports []fileReport, _ reportOptions) {
	out.WriteString("<?xml version='1.0' encoding='UTF-8'?>\n<checkstyle version='4.3'>\n")
	for _, r := range reports {
		fmt.Fprintf(out, "<file name='%s' >\n", xmlAttribute(r.name))
		for _, f := range r.findings {
			at := placeOf(r.doc, f.Span.Start)
			fmt.Fprintf(out, "<error line='%d' column='%d' severity='%s' message='%s' source='ShellCheck.%s' />\n",
				at.line, at.column, levels[f.Severity].checkstyle, xmlAttribute(f.Message), f.Code)
		}
		out.WriteString("</file>\n")
	}
	out.WriteString("</checkstyle>\n")
}

func xmlAttribute(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' || r == '.' || r == '/' {
			sb.WriteRune(r)
			continue
		}
		sb.WriteString("&#" + strconv.Itoa(int(r)) + ";")
	}
	return sb.String()
}

func fixedText(r fileReport) string {
	edits := actions.FixAll(actions.FindingDiagnostics(r.findings))
	if len(edits) == 0 {
		return r.doc.Text()
	}
	return textedit.Apply(r.doc.Text(), edits)
}

func writeDiff(out *bytes.Buffer, reports []fileReport, _ reportOptions) {
	for _, r := range reports {
		from, to := diffNames(r.name)
		if patch := format.Unified(from, to, r.doc.Text(), fixedText(r)); patch != "" {
			out.WriteString(patch)
			out.WriteByte('\n')
		}
	}
}

func diffNames(name string) (from, to string) {
	if filepath.IsAbs(name) {
		return name, name
	}
	return "a/" + name, "b/" + name
}

func writeTTY(out *bytes.Buffer, reports []fileReport, opts reportOptions) {
	paint := func(code, s string) string {
		if !opts.color {
			return s
		}
		return code + s + ansiReset
	}
	for _, r := range reports {
		text := r.doc.Text()
		for _, group := range lineGroups(r) {
			first := group[0]
			lineAt := lineStart(text, first.Span.Start)
			source := text[lineAt:lineEnd(text, lineAt)]
			fmt.Fprintf(out, "\n%s\n%s\n", paint(ansiBold, fmt.Sprintf("In %s line %d:", r.name, placeOf(r.doc, first.Span.Start).line)), paint(ansiReset, source))
			for _, f := range group {
				indent := strings.Repeat(" ", visualWidth(text[lineAt:f.Span.Start]))
				out.WriteString(paint(levels[f.Severity].color, indent+caret(text, f.Span)+" "+f.Code.String()+" ("+f.Severity.String()+"): "+f.Message) + "\n")
			}
			if suggestion, ok := didYouMean(text, lineAt, group); ok {
				fmt.Fprintf(out, "\n%s\n%s\n", paint(ansiBold, "Did you mean:"), suggestion)
			}
			out.WriteByte('\n')
		}
	}
	writeWikiLinks(out, reports, opts.wikiLinks)
}

func lineGroups(r fileReport) [][]lint.Finding {
	var groups [][]lint.Finding
	text := r.doc.Text()
	for _, f := range r.findings {
		if n := len(groups); n > 0 && lineStart(text, groups[n-1][0].Span.Start) == lineStart(text, f.Span.Start) {
			groups[n-1] = append(groups[n-1], f)
			continue
		}
		groups = append(groups, []lint.Finding{f})
	}
	return groups
}

func lineStart(text string, offset int) int {
	return strings.LastIndexByte(text[:offset], '\n') + 1
}

func lineEnd(text string, offset int) int {
	if i := strings.IndexByte(text[offset:], '\n'); i >= 0 {
		return offset + i
	}
	return len(text)
}

func visualWidth(s string) int {
	width := 0
	for _, r := range s {
		if r == '\t' {
			width += tabStop - width%tabStop
			continue
		}
		width++
	}
	return width
}

func caret(text string, span parser.Span) string {
	if strings.Contains(text[span.Start:span.End], "\n") {
		return "^--"
	}
	n := utf8.RuneCountInString(text[span.Start:span.End])
	if n <= 2 {
		return "^--"
	}
	return "^" + strings.Repeat("-", n-2) + "^"
}

func didYouMean(text string, lineAt int, group []lint.Finding) (string, bool) {
	fixes := make([]lint.Finding, 0, len(group))
	for _, f := range group {
		if len(f.Fixes) > 0 {
			fixes = append(fixes, f)
		}
	}
	edits := actions.FixAll(actions.FindingDiagnostics(fixes))
	if len(edits) == 0 {
		return "", false
	}
	start, end := lineAt, lineEnd(text, lineAt)
	for _, e := range edits {
		start = min(start, lineStart(text, e.Start))
		end = max(end, lineEnd(text, e.End))
	}
	shifted := make([]textedit.Edit, len(edits))
	for i, e := range edits {
		shifted[i] = textedit.Edit{Start: e.Start - start, End: e.End - start, NewText: e.NewText}
	}
	return textedit.Apply(text[start:end], shifted), true
}

const wikiMessageLimit = 33

func writeWikiLinks(out *bytes.Buffer, reports []fileReport, limit int) {
	first := map[lint.Code]lint.Finding{}
	for _, r := range reports {
		for _, f := range r.findings {
			if _, seen := first[f.Code]; !seen {
				first[f.Code] = f
			}
		}
	}
	if limit == 0 || len(first) == 0 {
		return
	}
	picked := slices.SortedFunc(func(yield func(lint.Finding) bool) {
		for _, f := range first {
			if !yield(f) {
				return
			}
		}
	}, func(a, b lint.Finding) int {
		return cmp.Or(cmp.Compare(a.Severity, b.Severity), cmp.Compare(a.Code, b.Code))
	})
	out.WriteString("For more information:\n")
	for _, f := range picked[:min(limit, len(picked))] {
		message := f.Message
		if utf8.RuneCountInString(message) > wikiMessageLimit {
			message = string([]rune(message)[:wikiMessageLimit]) + "..."
		}
		fmt.Fprintf(out, "  %s -- %s\n", f.Code.Reference(), message)
	}
}

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool       sarifTool     `json:"tool"`
	ColumnKind string        `json:"columnKind"`
	Results    []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID      string `json:"id"`
	HelpURI string `json:"helpUri"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	RuleIndex int             `json:"ruleIndex"`
	Level     string          `json:"level"`
	Message   sarifMessage    `json:"message"`
	Locations []sarifLocation `json:"locations"`
	Fixes     []sarifFix      `json:"fixes,omitempty"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifact `json:"artifactLocation"`
	Region           sarifRegion   `json:"region"`
}

type sarifArtifact struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn"`
	EndLine     int `json:"endLine"`
	EndColumn   int `json:"endColumn"`
}

type sarifFix struct {
	Description     sarifMessage          `json:"description"`
	ArtifactChanges []sarifArtifactChange `json:"artifactChanges"`
}

type sarifArtifactChange struct {
	ArtifactLocation sarifArtifact      `json:"artifactLocation"`
	Replacements     []sarifReplacement `json:"replacements"`
}

type sarifReplacement struct {
	DeletedRegion   sarifRegion  `json:"deletedRegion"`
	InsertedContent sarifMessage `json:"insertedContent"`
}

const (
	sarifSchema    = "https://json.schemastore.org/sarif-2.1.0.json"
	sarifVersion   = "2.1.0"
	informationURI = "https://github.com/stubbedev/shell-lsp"
)

func writeSARIF(out *bytes.Buffer, reports []fileReport, opts reportOptions) {
	var codes []lint.Code
	for _, r := range reports {
		for _, f := range r.findings {
			codes = append(codes, f.Code)
		}
	}
	slices.Sort(codes)
	codes = slices.Compact(codes)
	rules := make([]sarifRule, len(codes))
	for i, code := range codes {
		rules[i] = sarifRule{ID: code.String(), HelpURI: code.Reference()}
	}
	results := []sarifResult{}
	for _, r := range reports {
		artifact := sarifArtifact{URI: filepath.ToSlash(r.name)}
		region := func(start, end int) sarifRegion {
			from, to := placeOf(r.doc, start), placeOf(r.doc, end)
			return sarifRegion{StartLine: from.line, StartColumn: from.column, EndLine: to.line, EndColumn: to.column}
		}
		for _, f := range r.findings {
			index, _ := slices.BinarySearch(codes, f.Code)
			result := sarifResult{
				RuleID: f.Code.String(), RuleIndex: index, Level: levels[f.Severity].sarif, Message: sarifMessage{Text: f.Message},
				Locations: []sarifLocation{{PhysicalLocation: sarifPhysicalLocation{ArtifactLocation: artifact, Region: region(f.Span.Start, f.Span.End)}}},
			}
			for _, fix := range f.Fixes {
				change := sarifArtifactChange{ArtifactLocation: artifact, Replacements: make([]sarifReplacement, len(fix.Edits))}
				for i, e := range fix.Edits {
					change.Replacements[i] = sarifReplacement{DeletedRegion: region(e.Start, e.End), InsertedContent: sarifMessage{Text: e.NewText}}
				}
				result.Fixes = append(result.Fixes, sarifFix{Description: sarifMessage{Text: fix.Title}, ArtifactChanges: []sarifArtifactChange{change}})
			}
			results = append(results, result)
		}
	}
	encodeJSON(out, sarifLog{
		Schema:  sarifSchema,
		Version: sarifVersion,
		Runs: []sarifRun{{
			Tool:       sarifTool{Driver: sarifDriver{Name: programName, Version: opts.version, InformationURI: informationURI, Rules: rules}},
			ColumnKind: "unicodeCodePoints",
			Results:    results,
		}},
	})
}
