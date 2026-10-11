package bats

import (
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
)

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

type Report struct {
	Planned    int
	HasPlan    bool
	Results    []Result
	Bailed     bool
	BailReason string
}

type Tally struct {
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
	Todo    int `json:"todo"`
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
	if p.report.Bailed {
		return false
	}
	if reason, ok := strings.CutPrefix(line, bailOut); ok {
		p.report.Bailed, p.report.BailReason = true, strings.TrimSpace(reason)
		return false
	}
	if m := planLine.FindStringSubmatch(line); m != nil {
		planned, err := strconv.Atoi(m[1])
		if err == nil {
			p.report.Planned, p.report.HasPlan = planned, true
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
		switch res.Status {
		case Passed:
			t.Passed++
		case Failed:
			t.Failed++
		case Skipped:
			t.Skipped++
		case Todo:
			t.Todo++
		}
	}
	return t
}

func (r Report) Empty() bool {
	return !r.HasPlan && !r.Bailed && len(r.Results) == 0
}

func (r Report) PlanMismatch() bool {
	return r.HasPlan && r.Planned != len(r.Results)
}

func (r Report) OK() bool {
	return r.HasPlan && !r.Bailed && !r.PlanMismatch() && r.Tally().Failed == 0
}

func (r Report) Summary() string {
	t := r.Tally()
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d passed, %d failed, %d skipped", t.Passed, t.Failed, t.Skipped)
	if t.Todo > 0 {
		fmt.Fprintf(&sb, ", %d todo", t.Todo)
	}
	switch {
	case !r.HasPlan:
		sb.WriteString("; no TAP plan was reported")
	case r.PlanMismatch():
		fmt.Fprintf(&sb, "; planned %d tests but %d reported", r.Planned, len(r.Results))
	}
	if r.Bailed {
		sb.WriteString("; bailed out")
		if r.BailReason != "" {
			sb.WriteString(": " + r.BailReason)
		}
	}
	return sb.String()
}
