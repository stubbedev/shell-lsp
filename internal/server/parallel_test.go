package server

import (
	"sync/atomic"
	"testing"
)

func TestParallelVisitsEachIndexOnce(t *testing.T) {
	t.Parallel()

	for _, u := range []urgency{background, interactive} {
		for _, n := range []int{0, 1, 3, 1000} {
			counts := make([]atomic.Int32, n)
			parallel(u, n, func(i int) { counts[i].Add(1) })
			for i := range counts {
				if got := counts[i].Load(); got != 1 {
					t.Fatalf("urgency %d, n=%d: index %d visited %d times", u, n, i, got)
				}
			}
		}
	}
}
