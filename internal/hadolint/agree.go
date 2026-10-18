package hadolint

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/stubbedev/shell-lsp/internal/host/dockerfile"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

type Reported struct {
	Code string
	Line int
}

var aliasedBy = sync.OnceValue(func() map[lint.Code][]lint.Code {
	out := map[lint.Code][]lint.Code{}
	for _, dl := range slices.Sorted(maps.Keys(aliases)) {
		for _, sl := range aliases[dl] {
			out[sl] = append(out[sl], dl)
		}
	}
	return out
})

var replacedBy = map[string]string{"CMD": "DL4003", "ENTRYPOINT": "DL4004", "HEALTHCHECK": "DL3012"}

func instructionAt(f *dockerfile.File, offset int) *dockerfile.Instruction {
	for _, in := range f.Instructions {
		if in.Span.Start <= offset && offset < max(in.Span.End, in.Span.Start+1) {
			return in
		}
	}
	return nil
}

func reportedAs(f *dockerfile.File, finding lint.Finding, in *dockerfile.Instruction) (lint.Code, bool) {
	if finding.Code.Hadolint() {
		return finding.Code, true
	}
	codes := aliasedBy()[finding.Code]
	pick := func(text string) (lint.Code, bool) {
		code := mustCode(text)
		return code, slices.Contains(codes, code)
	}
	switch {
	case len(codes) == 0:
		return 0, false
	case len(codes) == 1:
		return codes[0], true
	case in == nil:
		return pick("DL1000")
	case finding.Code == lint.CodeDockerMultipleInstructions:
		return pick(replacedBy[in.Name])
	case in.Name == "ONBUILD":
		return pick("DL3043")
	case slices.Contains(f.Preamble(), in):
		return pick("DL3061")
	}
	return pick("DL1000")
}

func lineOf(text string, offset int) int { return strings.Count(text[:offset], "\n") + 1 }

func Keys(f *dockerfile.File, findings []lint.Finding) []string {
	set := map[string]bool{}
	for _, finding := range findings {
		in := instructionAt(f, finding.Span.Start)
		code, ok := reportedAs(f, finding, in)
		if !ok {
			continue
		}
		offset := finding.Span.Start
		if in != nil {
			offset = in.Span.Start
		}
		set[code.String()+" "+strconv.Itoa(lineOf(f.Text, offset))] = true
	}
	return slices.Sorted(maps.Keys(set))
}

func ReportedKeys(reported []Reported) []string {
	set := map[string]bool{}
	for _, r := range reported {
		if strings.HasPrefix(r.Code, "DL") {
			set[r.Code+" "+strconv.Itoa(r.Line)] = true
		}
	}
	return slices.Sorted(maps.Keys(set))
}

func Disagreements(f *dockerfile.File, reported []Reported, findings []lint.Finding) []string {
	theirs, ours := ReportedKeys(reported), Keys(f, findings)
	var out []string
	for _, k := range theirs {
		if !slices.Contains(ours, k) {
			out = append(out, fmt.Sprintf("Hadolint reports %s, but the analysis does not", k))
		}
	}
	for _, k := range ours {
		if !slices.Contains(theirs, k) {
			out = append(out, fmt.Sprintf("the analysis reports %s, but Hadolint does not", k))
		}
	}
	return out
}
