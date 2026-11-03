package parallel

import (
	"sync"
	"sync/atomic"
)

func For(workers, n int, work func(i int)) {
	var next atomic.Int64
	var wg sync.WaitGroup
	for range min(max(workers, 1), n) {
		wg.Go(func() {
			for i := int(next.Add(1)) - 1; i < n; i = int(next.Add(1)) - 1 {
				work(i)
			}
		})
	}
	wg.Wait()
}

type Budget struct {
	mu       sync.Mutex
	freed    sync.Cond
	capacity int64
	held     int64
}

func NewBudget(capacity int64) *Budget {
	b := &Budget{capacity: capacity}
	b.freed.L = &b.mu
	return b
}

func (b *Budget) Hold(amount int64) (release func()) {
	amount = min(amount, b.capacity)
	b.mu.Lock()
	defer b.mu.Unlock()
	for b.held+amount > b.capacity {
		b.freed.Wait()
	}
	b.held += amount
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.held -= amount
		b.freed.Broadcast()
	}
}
