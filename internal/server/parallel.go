package server

import (
	"runtime"
	"sync"
	"sync/atomic"
)

type urgency uint8

const (
	background urgency = iota
	interactive
)

func (u urgency) workers() int {
	if u == background {
		return 1
	}
	return runtime.GOMAXPROCS(0)
}

func parallel(u urgency, n int, work func(i int)) {
	var next atomic.Int64
	var wg sync.WaitGroup
	for range min(u.workers(), n) {
		wg.Go(func() {
			for i := int(next.Add(1)) - 1; i < n; i = int(next.Add(1)) - 1 {
				work(i)
			}
		})
	}
	wg.Wait()
}
