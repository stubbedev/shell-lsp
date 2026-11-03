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

func TestBudgetNeverHoldsMoreThanItsCapacity(t *testing.T) {
	t.Parallel()

	const capacity = 10
	budget := NewBudget(capacity)
	var held, highest atomic.Int64
	For(16, 400, func(i int) {
		amount := min(int64(i%15+1), capacity)
		release := budget.Hold(int64(i%15 + 1))
		now := held.Add(amount)
		for seen := highest.Load(); now > seen && !highest.CompareAndSwap(seen, now); seen = highest.Load() {
		}
		held.Add(-amount)
		release()
	})
	if got := highest.Load(); got > capacity {
		t.Errorf("%d held at once, capacity %d", got, capacity)
	}
}
