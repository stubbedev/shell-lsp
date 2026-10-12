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

func InsertLine(text string, at int, line string) Edit {
	line += "\n"
	if at == len(text) && text != "" && !strings.HasSuffix(text, "\n") {
		line = "\n" + line
	}
	return Edit{Start: at, End: at, NewText: line}
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
		if selfOverlapping(group) || slices.ContainsFunc(group, func(e Edit) bool { return overlapsKept(e, kept) }) {
			continue
		}
		for _, e := range group {
			kept = slices.Insert(kept, insertionPoint(kept, e), e)
		}
	}
	return kept
}

func insertionPoint(kept []Edit, e Edit) int {
	if n := len(kept); n == 0 || byPosition(kept[n-1], e) <= 0 {
		return n
	}
	i, _ := slices.BinarySearchFunc(kept, e, func(k, e Edit) int {
		if byPosition(k, e) <= 0 {
			return -1
		}
		return 1
	})
	return i
}

func selfOverlapping(group []Edit) bool {
	sorted := slices.SortedStableFunc(slices.Values(group), byPosition)
	for i := 1; i < len(sorted); i++ {
		if sorted[i].Start < sorted[i-1].End {
			return true
		}
	}
	return false
}

func overlapsKept(e Edit, kept []Edit) bool {
	i, _ := slices.BinarySearchFunc(kept, e.Start, func(k Edit, start int) int {
		if k.End <= start {
			return -1
		}
		return 1
	})
	if i < len(kept) && overlaps(e, kept[i]) {
		return true
	}
	return i > 0 && overlaps(e, kept[i-1])
}

func overlaps(e, o Edit) bool {
	if e.Start < o.End && o.Start < e.End {
		return true
	}
	return e.Start == e.End && o.Start == o.End && e.Start == o.Start
}
