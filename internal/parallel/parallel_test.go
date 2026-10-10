package parallel

import (
	"sync/atomic"
	"testing"
)

func TestForVisitsEachIndexOnce(t *testing.T) {
	t.Parallel()

	for _, workers := range []int{0, 1, 4, 64} {
		for _, n := range []int{0, 1, 3, 1000} {
			counts := make([]atomic.Int32, n)
			For(workers, n, func(i int) { counts[i].Add(1) })
			for i := range counts {
				if got := counts[i].Load(); got != 1 {
					t.Fatalf("workers=%d, n=%d: index %d visited %d times", workers, n, i, got)
				}
			}
		}
	}
}
