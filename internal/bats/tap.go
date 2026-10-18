package bats

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/scan"
)

type Status uint8

const (
	Passed Status = iota
	Failed
	Skipped
	Todo
	statusCount
)

var statusNames = [statusCount]string{Passed: "passed", Failed: "failed", Skipped: "skipped", Todo: "todo"}

type Frame struct {
	File string
	Line int
}

type Result struct {
	Number int
	Name   string
	Status Status
	Reason string
	Output []string
	Frames []Frame
}

type Plan struct {
	Tests int
}

type Bail struct {
	Reason string
}

type Report struct {
	Plan    *Plan
	Results []Result
	Bail    *Bail
}

type Tally [statusCount]int

func (t Tally) Counts() map[string]int {
	counts := make(map[string]int, len(t))
	for s, n := range t {
		counts[statusNames[s]] = n
	}
	return counts
}

const (
	bailOut       = "Bail out!"
	lineReference = ", line "
)

type Parser struct {
	report Report
}

func (p *Parser) Line(line string) bool {
	line = strings.TrimSuffix(line, "\r")
	if p.report.Bail != nil {
		return false
	}
	if reason, ok := strings.CutPrefix(line, bailOut); ok {
		p.report.Bail = &Bail{Reason: strings.TrimSpace(reason)}
		return false
	}
	if planned, ok := plan(line); ok {
		p.report.Plan = &Plan{Tests: planned}
		return false
	}
	if r, ok := testPoint(line, len(p.report.Results)+1); ok {
		p.report.Results = append(p.report.Results, r)
		return true
	}
	if comment, ok := strings.CutPrefix(line, "#"); ok && len(p.report.Results) > 0 {
		p.comment(&p.report.Results[len(p.report.Results)-1], strings.TrimPrefix(comment, " "))
	}
	return false
}

func (p *Parser) Report() Report { return p.report }

func (p *Parser) comment(r *Result, text string) {
	if f, ok := frame(text); ok {
		r.Frames = append(r.Frames, f)
		return
	}
	r.Output = append(r.Output, text)
}

func plan(line string) (int, bool) {
	count, ok := strings.CutPrefix(line, "1..")
	if !ok {
		return 0, false
	}
	digits, rest := number(count)
	if digits == "" || rest != "" && !scan.Blank(rest[0]) {
		return 0, false
	}
	tests, err := strconv.Atoi(digits)
	if err != nil {
		return 0, false
	}
	return tests, true
}

func number(s string) (string, string) {
	end := scan.While(s, scan.Digit)
	return s[:end], s[end:]
}

func testPoint(line string, position int) (Result, bool) {
	rest, failed := strings.CutPrefix(line, "not ")
	rest, ok := strings.CutPrefix(rest, "ok")
	if !ok || rest != "" && !scan.Blank(rest[0]) {
		return Result{}, false
	}
	r := Result{Number: position, Status: Passed}
	if failed {
		r.Status = Failed
	}
	rest = scan.TrimBlanks(rest)
	if digits, after := number(rest); digits != "" && (after == "" || scan.Blank(after[0])) {
		if n, err := strconv.Atoi(digits); err == nil {
			r.Number = n
		}
		rest = scan.TrimBlanks(after)
	}
	if after, ok := strings.CutPrefix(rest, "-"); ok && (after == "" || scan.Blank(after[0])) {
		rest = scan.TrimBlanks(after)
	}
	describe(&r, rest)
	return r, true
}

func frame(text string) (Frame, bool) {
	if len(text) < 2 || text[0] != '(' && text[0] != ' ' {
		return Frame{}, false
	}
	var location string
	switch text[len(text)-1] {
	case ')':
		location = "in test file "
	case ',':
		location = "in file "
	default:
		return Frame{}, false
	}
	body := text[1 : len(text)-1]
	if called, ok := strings.CutPrefix(body, "from function "); ok {
		at := strings.Index(called, " "+location)
		if at < 0 {
			return Frame{}, false
		}
		body = called[at+1:]
	}
	reference, ok := strings.CutPrefix(body, location)
	if !ok {
		return Frame{}, false
	}
	at := strings.LastIndex(reference, lineReference)
	if at <= 0 {
		return Frame{}, false
	}
	digits, rest := number(reference[at+len(lineReference):])
	line, err := strconv.Atoi(digits)
	if rest != "" || err != nil || line < 1 {
		return Frame{}, false
	}
	return Frame{File: reference[:at], Line: line}, true
}

func describe(r *Result, description string) {
	name, directive, hasDirective := splitDirective(description)
	r.Name = name
	if !hasDirective {
		return
	}
	keyword, reason, _ := strings.Cut(directive, " ")
	switch {
	case strings.HasPrefix(strings.ToLower(keyword), "skip"):
		r.Status, r.Reason = Skipped, strings.TrimSpace(reason)
	case strings.HasPrefix(strings.ToLower(keyword), "todo"):
		r.Status, r.Reason = Todo, strings.TrimSpace(reason)
	default:
		r.Name = unescape(description)
	}
}

func splitDirective(description string) (string, string, bool) {
	for i := 0; i < len(description); i++ {
		switch description[i] {
		case '\\':
			i++
		case '#':
			return strings.TrimSpace(unescape(description[:i])), strings.TrimSpace(description[i+1:]), true
		}
	}
	return strings.TrimSpace(unescape(description)), "", false
}

func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && (s[i+1] == '#' || s[i+1] == '\\') {
			i++
		}
		sb.WriteByte(s[i])
	}
	return sb.String()
}

func (r Report) Tally() Tally {
	var t Tally
	for _, res := range r.Results {
		t[res.Status]++
	}
	return t
}

func (r Report) Empty() bool {
	return r.Plan == nil && r.Bail == nil && len(r.Results) == 0
}

func (r Report) PlanMismatch() bool {
	return r.Plan != nil && r.Plan.Tests != len(r.Results)
}

func (r Report) OK() bool {
	return r.Plan != nil && r.Bail == nil && !r.PlanMismatch() && r.Tally()[Failed] == 0
}

func (r Report) Summary() string {
	t := r.Tally()
	var sb strings.Builder
	for s, n := range t {
		if Status(s) == Todo && n == 0 {
			continue
		}
		if s > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "%d %s", n, statusNames[s])
	}
	switch {
	case r.Plan == nil:
		sb.WriteString("; no TAP plan was reported")
	case r.PlanMismatch():
		fmt.Fprintf(&sb, "; planned %d tests but %d reported", r.Plan.Tests, len(r.Results))
	}
	if r.Bail != nil {
		sb.WriteString("; bailed out")
		if r.Bail.Reason != "" {
			sb.WriteString(": " + r.Bail.Reason)
		}
	}
	return sb.String()
}
