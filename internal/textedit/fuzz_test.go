package textedit

import (
	"slices"
	"strings"
	"testing"
)

func referenceNonOverlapping(groups [][]Edit) []Edit {
	var kept []Edit
	for _, group := range groups {
		if selfOverlapping(group) || slices.ContainsFunc(group, func(e Edit) bool {
			return slices.ContainsFunc(kept, func(o Edit) bool { return overlaps(e, o) })
		}) {
			continue
		}
		kept = append(kept, group...)
	}
	slices.SortStableFunc(kept, byPosition)
	return kept
}

func FuzzNonOverlapping(f *testing.F) {
	f.Add([]byte{3, 0, 0, 2, 2, 1, 1, 3, 0, 0})
	f.Add([]byte{2, 5, 11, 5, 5, 1, 11, 11})
	f.Fuzz(func(t *testing.T, script []byte) {
		var groups [][]Edit
		for len(script) >= 3 && len(groups) < 64 {
			size := int(script[0]%3) + 1
			script = script[1:]
			var group []Edit
			for ; size > 0 && len(script) >= 2; size-- {
				start := int(script[0] % 16)
				end := start + int(script[1]%4)
				group = append(group, Edit{Start: start, End: end, NewText: string(rune('a' + len(groups)))})
				script = script[2:]
			}
			groups = append(groups, group)
		}
		got, want := NonOverlapping(groups), referenceNonOverlapping(groups)
		if !slices.Equal(got, want) {
			t.Fatalf("NonOverlapping(%v) = %v, want %v", groups, got, want)
		}
		Apply(strings.Repeat("x", 20), got)
	})
}
