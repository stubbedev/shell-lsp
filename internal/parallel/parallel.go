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
