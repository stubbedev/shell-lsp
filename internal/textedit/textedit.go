package textedit

import (
	"cmp"
	"slices"
	"strings"
)

type Edit struct {
	Start   int
	End     int
	NewText string
}

func Apply(text string, edits []Edit) string {
	sorted := slices.Clone(edits)
	slices.SortStableFunc(sorted, byPosition)
	var sb strings.Builder
	last := 0
	for _, e := range sorted {
		sb.WriteString(text[last:e.Start])
		sb.WriteString(e.NewText)
		last = e.End
	}
	sb.WriteString(text[last:])
	return sb.String()
}

func byPosition(a, b Edit) int {
	return cmp.Or(cmp.Compare(a.Start, b.Start), cmp.Compare(a.End, b.End))
}

func NonOverlapping(groups [][]Edit) []Edit {
	var kept []Edit
	for _, group := range groups {
		if slices.ContainsFunc(group, func(e Edit) bool { return overlapsAny(e, kept) }) {
			continue
		}
		kept = append(kept, group...)
	}
	slices.SortStableFunc(kept, byPosition)
	return kept
}

func overlapsAny(e Edit, others []Edit) bool {
	for _, o := range others {
		if e.Start < o.End && o.Start < e.End {
			return true
		}
		if e.Start == e.End && o.Start == o.End && e.Start == o.Start {
			return true
		}
	}
	return false
}
