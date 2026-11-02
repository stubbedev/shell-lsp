package nixeval

import (
	"container/list"
	"context"
	"fmt"
	"sync"
	"time"
)

const (
	entryOverhead = 1 << 10
	failureTTL    = 30 * time.Second
)

type fingerprint [32]byte

type request struct {
	dir         string
	fingerprint fingerprint
	detail      string
}

type computed[T any] struct {
	value T
	cost  int
}

type entry[T any] struct {
	request  request
	done     chan struct{}
	cancel   context.CancelFunc
	waiters  int
	finished bool
	value    T
	err      error
	cost     int
	expires  time.Time
	element  *list.Element
}

type cache[T any] struct {
	budget  int
	timeout time.Duration
	now     func() time.Time

	mu      sync.Mutex
	entries map[request]*entry[T]
	recent  list.List
	used    int
}

func newCache[T any](budget int, timeout time.Duration, now func() time.Time) *cache[T] {
	return &cache[T]{budget: budget, timeout: timeout, now: now, entries: map[request]*entry[T]{}}
}

func (c *cache[T]) get(ctx context.Context, req request, compute func(context.Context) (computed[T], error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	e := c.join(req, compute)
	select {
	case <-e.done:
		return e.value, e.err
	case <-ctx.Done():
		c.leave(e)
		return zero, fmt.Errorf("waiting for the result: %w", ctx.Err())
	}
}

func (c *cache[T]) join(req request, compute func(context.Context) (computed[T], error)) *entry[T] {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[req]
	if ok && e.finished && e.err != nil && !c.now().Before(e.expires) {
		c.removeLocked(e)
		ok = false
	}
	if !ok {
		e = c.startLocked(req, compute)
	}
	e.waiters++
	if e.finished {
		c.recent.MoveToFront(e.element)
	}
	return e
}

func (c *cache[T]) startLocked(req request, compute func(context.Context) (computed[T], error)) *entry[T] {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	e := &entry[T]{request: req, done: make(chan struct{}), cancel: cancel}
	c.entries[req] = e
	go func() {
		defer cancel()
		result, err := compute(ctx)
		c.finish(e, result, err)
	}()
	return e
}

func (c *cache[T]) finish(e *entry[T], result computed[T], err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	defer close(e.done)
	e.value, e.err, e.finished = result.value, err, true
	if c.entries[e.request] != e {
		return
	}
	e.cost = result.cost + entryOverhead
	if err != nil {
		e.expires = c.now().Add(failureTTL)
	}
	e.element = c.recent.PushFront(e)
	c.used += e.cost
	for c.used > c.budget && c.recent.Len() > 1 {
		oldest, ok := c.recent.Back().Value.(*entry[T])
		if !ok {
			panic("nixeval: cache list holds a foreign value")
		}
		c.removeLocked(oldest)
	}
}

func (c *cache[T]) leave(e *entry[T]) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e.waiters--
	if e.waiters > 0 || e.finished {
		return
	}
	e.cancel()
	if c.entries[e.request] == e {
		delete(c.entries, e.request)
	}
}

func (c *cache[T]) removeLocked(e *entry[T]) {
	delete(c.entries, e.request)
	if e.element != nil {
		c.recent.Remove(e.element)
		c.used -= e.cost
	}
}

func (c *cache[T]) invalidate(dir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for req, e := range c.entries {
		if req.dir == dir {
			c.removeLocked(e)
		}
	}
}
