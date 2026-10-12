package scan

import (
	"slices"
	"testing"
)

func TestForwardVisitsEveryStep(t *testing.T) {
	t.Parallel()

	var visited []int
	Forward(2, 9, func(at int) int {
		visited = append(visited, at)
		return at + at%3 + 1
	})
	if want := []int{2, 5, 8}; !slices.Equal(visited, want) {
		t.Errorf("visited %v, want %v", visited, want)
	}
}

func TestForwardRefusesAStepThatStays(t *testing.T) {
	t.Parallel()

	for _, step := range []func(int) int{func(at int) int { return at }, func(at int) int { return at - 1 }} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("a step that does not advance was accepted")
				}
			}()
			Forward(0, 10, step)
		}()
	}
}
