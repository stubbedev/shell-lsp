package nixeval

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

const testTimeout = time.Minute

func testKey(name string) contentKey {
	return digestOf(name)
}

func newTestCache(t *testing.T, budget int) (*cache[string], *lifetime) {
	t.Helper()
	life := newLifetime()
	t.Cleanup(life.close)
	return newCache[string](life, budget, testTimeout, time.Now), life
}

func constant(value string, cost int) computeFunc[string] {
	return func(context.Context, task) (computed[string], error) {
		return computed[string]{value: value, cost: cost}, nil
	}
}

func TestAwaitDeduplicatesConcurrentRequests(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		c, _ := newTestCache(t, 1<<20)
		var calls atomic.Int32
		release := make(chan struct{})
		compute := func(context.Context, task) (computed[string], error) {
			calls.Add(1)
			<-release
			return computed[string]{value: "v"}, nil
		}
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				got, err := c.await(t.Context(), testKey("a"), compute)
				if err != nil || got != "v" {
					t.Errorf("await() = %q, %v", got, err)
				}
			})
		}
		synctest.Wait()
		close(release)
		wg.Wait()
		if got := calls.Load(); got != 1 {
			t.Errorf("computed %d times, want once", got)
		}
	})
}

func TestAwaitServesFinishedResultsAndSeparatesKeys(t *testing.T) {
	t.Parallel()

	c, _ := newTestCache(t, 1<<20)
	var calls atomic.Int32
	compute := func(context.Context, task) (computed[string], error) {
		calls.Add(1)
		return computed[string]{value: "v"}, nil
	}
	for range 3 {
		if _, err := c.await(t.Context(), testKey("a"), compute); err != nil {
			t.Fatal(err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("computed %d times, want once", got)
	}
	if _, err := c.await(t.Context(), testKey("b"), compute); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("computed %d times, want 2 for two distinct keys", got)
	}
}

func TestCacheEvictsTheLeastRecentlyUsedPastItsBudget(t *testing.T) {
	t.Parallel()

	c, _ := newTestCache(t, 3*(entryOverhead+100))
	for _, name := range []string{"a", "b", "c"} {
		if _, err := c.await(t.Context(), testKey(name), constant(name, 100)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.await(t.Context(), testKey("a"), constant("recomputed", 100)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.await(t.Context(), testKey("d"), constant("d", 100)); err != nil {
		t.Fatal(err)
	}
	got, err := c.await(t.Context(), testKey("a"), constant("recomputed", 100))
	if err != nil || got != "a" {
		t.Errorf("recently used entry = %q, %v, want it kept", got, err)
	}
	got, err = c.await(t.Context(), testKey("b"), constant("recomputed", 100))
	if err != nil || got != "recomputed" {
		t.Errorf("oldest entry = %q, %v, want it evicted", got, err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.used > c.budget {
		t.Errorf("used %d bytes over a budget of %d", c.used, c.budget)
	}
}

func TestCacheKeepsASingleOversizedResult(t *testing.T) {
	t.Parallel()

	c, _ := newTestCache(t, 10)
	if _, err := c.await(t.Context(), testKey("big"), constant("big", 1000)); err != nil {
		t.Fatal(err)
	}
	got, err := c.await(t.Context(), testKey("big"), constant("recomputed", 1000))
	if err != nil || got != "big" {
		t.Errorf("await() = %q, %v, want the oversized result kept", got, err)
	}
	if _, err := c.await(t.Context(), testKey("next"), constant("next", 1)); err != nil {
		t.Fatal(err)
	}
	got, err = c.await(t.Context(), testKey("big"), constant("recomputed", 1))
	if err != nil || got != "recomputed" {
		t.Errorf("await() = %q, %v, want the oversized result evicted by the next one", got, err)
	}
}

func TestFailuresAreRetriedOnlyAfterTheirTTL(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		c, _ := newTestCache(t, 1<<20)
		boom := errors.New("boom")
		var calls atomic.Int32
		compute := func(context.Context, task) (computed[string], error) {
			calls.Add(1)
			return computed[string]{}, boom
		}
		for range 2 {
			if _, err := c.await(t.Context(), testKey("a"), compute); !errors.Is(err, boom) {
				t.Fatalf("await() error = %v, want boom", err)
			}
		}
		if got := calls.Load(); got != 1 {
			t.Errorf("computed %d times inside the TTL, want once", got)
		}
		time.Sleep(failureTTL)
		if _, err := c.await(t.Context(), testKey("a"), compute); !errors.Is(err, boom) {
			t.Fatalf("await() error = %v, want boom", err)
		}
		if got := calls.Load(); got != 2 {
			t.Errorf("computed %d times after the TTL, want twice", got)
		}
	})
}

func TestARequestGivingUpLeavesTheWorkRunning(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		c, _ := newTestCache(t, 1<<20)
		release := make(chan struct{})
		var calls atomic.Int32
		compute := func(ctx context.Context, _ task) (computed[string], error) {
			calls.Add(1)
			select {
			case <-release:
				return computed[string]{value: "finished"}, nil
			case <-ctx.Done():
				return computed[string]{}, ctx.Err()
			}
		}
		short, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		if _, err := c.await(short, testKey("a"), compute); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("await() error = %v, want the deadline of the request", err)
		}
		close(release)
		got, err := c.await(t.Context(), testKey("a"), compute)
		if err != nil || got != "finished" {
			t.Errorf("await() = %q, %v, want the work that kept running", got, err)
		}
		if got := calls.Load(); got != 1 {
			t.Errorf("computed %d times, want once", got)
		}
	})
}

func TestWorkOutlivingTheTimeoutFails(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		c, _ := newTestCache(t, 1<<20)
		_, err := c.await(t.Context(), testKey("a"), func(ctx context.Context, _ task) (computed[string], error) {
			<-ctx.Done()
			return computed[string]{}, ctx.Err()
		})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("await() error = %v, want the deadline", err)
		}
	})
}

func TestANewKeyForASlotSupersedesTheRunningWork(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		c, _ := newTestCache(t, 1<<20)
		slot := slotKey(testKey("slot"))
		blocked := func(ctx context.Context, _ task) (computed[string], error) {
			<-ctx.Done()
			return computed[string]{}, ctx.Err()
		}
		old := c.join(testKey("old"), slot, foreground, blocked)
		synctest.Wait()
		newer := c.join(testKey("new"), slot, foreground, constant("new", 1))
		<-old.done
		<-newer.done
		if !errors.Is(old.err, ErrSuperseded) {
			t.Errorf("superseded work error = %v", old.err)
		}
		if newer.err != nil || newer.value != "new" {
			t.Errorf("newer work = %q, %v", newer.value, newer.err)
		}
		got, err := c.await(t.Context(), testKey("old"), constant("again", 1))
		if err != nil || got != "again" {
			t.Errorf("await(old) = %q, %v, want a superseded key to be computed afresh", got, err)
		}
	})
}

func TestClosingStopsTheWorkAndRefusesMore(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		c, life := newTestCache(t, 1<<20)
		e := c.join(testKey("a"), slotKey{}, foreground, func(ctx context.Context, _ task) (computed[string], error) {
			<-ctx.Done()
			return computed[string]{}, ctx.Err()
		})
		synctest.Wait()
		life.close()
		if !errors.Is(e.err, ErrClosed) {
			t.Errorf("running work error = %v, want closed", e.err)
		}
		if _, err := c.await(t.Context(), testKey("b"), constant("b", 1)); !errors.Is(err, ErrClosed) {
			t.Errorf("await() after close error = %v, want closed", err)
		}
	})
}
