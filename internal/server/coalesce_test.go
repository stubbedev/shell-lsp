package server

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestCoalescerRunsOnceMoreForCallsDuringARun(t *testing.T) {
	t.Parallel()

	c := newCoalescer()
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{}, 2)
	var runs atomic.Int32
	fn := func() {
		if runs.Add(1) == 1 {
			close(started)
			<-release
		}
		finished <- struct{}{}
	}
	c.run("key", fn)
	<-started
	for range 10 {
		c.run("key", fn)
	}
	close(release)
	<-finished
	<-finished
	deadline := time.Now().Add(5 * time.Second)
	for {
		c.mu.Lock()
		idle := len(c.running) == 0 && len(c.again) == 0
		c.mu.Unlock()
		if idle {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the coalescer never became idle")
		}
		time.Sleep(time.Millisecond)
	}
	if n := runs.Load(); n != 2 {
		t.Errorf("ran %d times, want the first run and one more for the calls made during it", n)
	}
}
