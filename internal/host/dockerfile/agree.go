package dockerfile

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/lint"
)

type BuildKitCheck struct {
	Rule string
	Line int
}

type placedCheck struct {
	code        lint.Code
	instruction int
}

func (f *File) instructionOnLine(line int) int {
	for i, in := range f.Instructions {
		if f.lineOf(in.Span.Start) <= line && line <= f.lineOf(max(in.Span.Start, in.Span.End-1)) {
			return i
		}
	}
	return -1
}

func (f *File) lineOf(offset int) int { return strings.Count(f.Text[:offset], "\n") + 1 }

func (f *File) CheckDisagreements(reported []BuildKitCheck, findings []lint.Finding) []string {
	want := map[placedCheck]bool{}
	for _, c := range reported {
		if code, mapped := RuleCodes[c.Rule]; mapped {
			want[placedCheck{code, f.instructionOnLine(c.Line)}] = true
		}
	}
	checked := slices.Collect(maps.Values(RuleCodes))
	got := map[placedCheck]bool{}
	for _, finding := range findings {
		if slices.Contains(checked, finding.Code) {
			got[placedCheck{finding.Code, f.instructionOnLine(f.lineOf(finding.Span.Start))}] = true
		}
	}
	var out []string
	for c := range want {
		if !got[c] {
			out = append(out, fmt.Sprintf("BuildKit reports %s on instruction %d, but the analysis does not", c.code, c.instruction+1))
		}
	}
	for c := range got {
		if !want[c] {
			out = append(out, fmt.Sprintf("the analysis reports %s on instruction %d, but BuildKit does not", c.code, c.instruction+1))
		}
	}
	slices.Sort(out)
	return out
}
