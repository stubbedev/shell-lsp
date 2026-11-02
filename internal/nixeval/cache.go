package nixeval

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	entryOverhead = 1 << 10
	failureTTL    = 30 * time.Second
)

type contentKey [32]byte

type slotKey [32]byte

type priority int

const (
	foreground priority = iota
	background
)

type lifetime struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	mu     sync.Mutex
	closed bool
	tasks  sync.WaitGroup
}

func newLifetime() *lifetime {
	ctx, cancel := context.WithCancelCause(context.Background())
	return &lifetime{ctx: ctx, cancel: cancel}
}

func (l *lifetime) start(run func()) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return false
	}
	l.tasks.Go(run)
	return true
}

func (l *lifetime) close() {
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()
	l.cancel(ErrClosed)
	l.tasks.Wait()
}

type task struct {
	priority priority
	promoted <-chan struct{}
	miss     func()
}

type computed[T any] struct {
	value T
	cost  int
}

type computeFunc[T any] func(context.Context, task) (computed[T], error)

type entry[T any] struct {
	key      contentKey
	slot     slotKey
	done     chan struct{}
	missed   chan struct{}
	promote  chan struct{}
	raised   bool
	cancel   context.CancelCauseFunc
	finished bool
	value    T
	err      error
	cost     int
	expires  time.Time
	element  *list.Element
}

func (e *entry[T]) refresh() *Refresh {
	return &Refresh{done: e.done, err: &e.err}
}

func (e *entry[T]) succeeded() bool {
	select {
	case <-e.done:
		return e.err == nil
	default:
		return false
	}
}

type cache[T any] struct {
	life    *lifetime
	budget  int
	timeout time.Duration
	now     func() time.Time

	mu      sync.Mutex
	entries map[contentKey]*entry[T]
	running map[slotKey]*entry[T]
	recent  list.List
	used    int
}

func newCache[T any](life *lifetime, budget int, timeout time.Duration, now func() time.Time) *cache[T] {
	return &cache[T]{
		life:    life,
		budget:  budget,
		timeout: timeout,
		now:     now,
		entries: map[contentKey]*entry[T]{},
		running: map[slotKey]*entry[T]{},
	}
}

func (c *cache[T]) join(key contentKey, slot slotKey, level priority, compute computeFunc[T]) *entry[T] {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if ok && e.finished && e.err != nil && !c.now().Before(e.expires) {
		c.removeLocked(e)
		ok = false
	}
	if !ok {
		e = c.startLocked(key, slot, level, compute)
	}
	if level == foreground && !e.raised {
		e.raised = true
		close(e.promote)
	}
	if e.element != nil {
		c.recent.MoveToFront(e.element)
	}
	return e
}

func (c *cache[T]) startLocked(key contentKey, slot slotKey, level priority, compute computeFunc[T]) *entry[T] {
	cancellable, cancel := context.WithCancelCause(c.life.ctx)
	ctx, stop := context.WithTimeout(cancellable, c.timeout)
	e := &entry[T]{key: key, slot: slot, done: make(chan struct{}), missed: make(chan struct{}), promote: make(chan struct{}), cancel: cancel}
	c.entries[key] = e
	if slot != (slotKey{}) {
		if previous := c.running[slot]; previous != nil {
			previous.cancel(ErrSuperseded)
		}
		c.running[slot] = e
	}
	var missOnce sync.Once
	work := task{priority: level, promoted: e.promote, miss: func() { missOnce.Do(func() { close(e.missed) }) }}
	started := c.life.start(func() {
		defer stop()
		defer cancel(nil)
		result, err := compute(ctx, work)
		c.finish(e, result, err, context.Cause(ctx))
	})
	if !started {
		stop()
		cancel(nil)
		c.finishLocked(e, computed[T]{}, ErrClosed, ErrClosed)
	}
	return e
}

func (c *cache[T]) finish(e *entry[T], result computed[T], err, cause error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.finishLocked(e, result, err, cause)
}

func (c *cache[T]) finishLocked(e *entry[T], result computed[T], err, cause error) {
	defer close(e.done)
	if c.running[e.slot] == e {
		delete(c.running, e.slot)
	}
	if err != nil && cause != nil && !errors.Is(cause, context.DeadlineExceeded) {
		e.err, e.finished = fmt.Errorf("%w: %w", cause, err), true
		if c.entries[e.key] == e {
			delete(c.entries, e.key)
		}
		return
	}
	e.value, e.err, e.finished = result.value, err, true
	if c.entries[e.key] != e {
		return
	}
	if err != nil {
		e.expires = c.now().Add(failureTTL)
	}
	c.rememberLocked(e, result.cost)
}

func (c *cache[T]) rememberLocked(e *entry[T], cost int) {
	e.cost = cost + entryOverhead
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

func (c *cache[T]) removeLocked(e *entry[T]) {
	delete(c.entries, e.key)
	if e.element != nil {
		c.recent.Remove(e.element)
		e.element = nil
		c.used -= e.cost
	}
}

func (c *cache[T]) succeeded(key contentKey) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var zero T
	e, ok := c.entries[key]
	if !ok || !e.finished || e.err != nil {
		return zero, false
	}
	c.recent.MoveToFront(e.element)
	return e.value, true
}

func (c *cache[T]) insert(key contentKey, result computed[T]) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[key]; ok {
		return
	}
	done := make(chan struct{})
	close(done)
	e := &entry[T]{key: key, done: done, missed: make(chan struct{}), promote: done, raised: true, finished: true, value: result.value}
	c.entries[key] = e
	c.rememberLocked(e, result.cost)
}

func (c *cache[T]) await(ctx context.Context, key contentKey, compute computeFunc[T]) (T, error) {
	var zero T
	e := c.join(key, slotKey{}, foreground, compute)
	select {
	case <-e.done:
		return e.value, e.err
	case <-ctx.Done():
		return zero, fmt.Errorf("waiting for the result: %w", context.Cause(ctx))
	}
}
