package bats

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
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

func (t Tally) MarshalJSON() ([]byte, error) {
	counts := make(map[string]int, len(t))
	for s, n := range t {
		counts[statusNames[s]] = n
	}
	return json.Marshal(counts)
}

var (
	planLine   = regexp.MustCompile(`^1\.\.(\d+)\b`)
	resultLine = regexp.MustCompile(`^(not )?ok\b(?:[ \t]+(\d+))?(?:[ \t]*-)?[ \t]*(.*)$`)
	frameLine  = regexp.MustCompile(`\bin (?:test )?file (.+), line (\d+)[,)]`)
)

const bailOut = "Bail out!"

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
	if m := planLine.FindStringSubmatch(line); m != nil {
		planned, err := strconv.Atoi(m[1])
		if err == nil {
			p.report.Plan = &Plan{Tests: planned}
		}
		return false
	}
	if m := resultLine.FindStringSubmatch(line); m != nil {
		p.report.Results = append(p.report.Results, result(m[1] != "", m[2], m[3], len(p.report.Results)+1))
		return true
	}
	if comment, ok := strings.CutPrefix(line, "#"); ok && len(p.report.Results) > 0 {
		p.comment(&p.report.Results[len(p.report.Results)-1], strings.TrimPrefix(comment, " "))
	}
	return false
}

func (p *Parser) Report() Report { return p.report }

func (p *Parser) comment(r *Result, text string) {
	m := frameLine.FindStringSubmatch(text)
	if m == nil {
		r.Output = append(r.Output, text)
		return
	}
	line, err := strconv.Atoi(m[2])
	if err != nil || line < 1 {
		r.Output = append(r.Output, text)
		return
	}
	r.Frames = append(r.Frames, Frame{File: m[1], Line: line})
}

func result(failed bool, number, description string, position int) Result {
	r := Result{Number: position, Status: Passed}
	if n, err := strconv.Atoi(number); err == nil {
		r.Number = n
	}
	if failed {
		r.Status = Failed
	}
	name, directive, hasDirective := splitDirective(description)
	r.Name = name
	if !hasDirective {
		return r
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
	return r
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
