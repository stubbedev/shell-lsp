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

func newTestCache(budget int) *cache[string] {
	return newCache[string](budget, testTimeout, time.Now)
}

func constant(value string, cost int) func(context.Context) (computed[string], error) {
	return func(context.Context) (computed[string], error) {
		return computed[string]{value: value, cost: cost}, nil
	}
}

func TestGetDeduplicatesConcurrentRequests(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		c := newTestCache(1 << 20)
		var calls atomic.Int32
		release := make(chan struct{})
		compute := func(context.Context) (computed[string], error) {
			calls.Add(1)
			<-release
			return computed[string]{value: "v"}, nil
		}
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				got, err := c.get(t.Context(), request{detail: "a"}, compute)
				if err != nil || got != "v" {
					t.Errorf("get() = %q, %v", got, err)
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

func TestGetServesFinishedResultsAndSeparatesRequests(t *testing.T) {
	t.Parallel()

	c := newTestCache(1 << 20)
	var calls int
	compute := func(context.Context) (computed[string], error) {
		calls++
		return computed[string]{value: "v"}, nil
	}
	for range 3 {
		if _, err := c.get(t.Context(), request{detail: "a"}, compute); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Errorf("computed %d times, want once", calls)
	}
	if _, err := c.get(t.Context(), request{detail: "b"}, compute); err != nil {
		t.Fatal(err)
	}
	if _, err := c.get(t.Context(), request{detail: "a", fingerprint: fingerprint{1}}, compute); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Errorf("computed %d times, want 3 for three distinct requests", calls)
	}
}

func TestCacheEvictsTheLeastRecentlyUsedPastItsBudget(t *testing.T) {
	t.Parallel()

	c := newTestCache(3 * (entryOverhead + 100))
	for _, detail := range []string{"a", "b", "c"} {
		if _, err := c.get(t.Context(), request{detail: detail}, constant(detail, 100)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.get(t.Context(), request{detail: "a"}, constant("recomputed", 100)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.get(t.Context(), request{detail: "d"}, constant("d", 100)); err != nil {
		t.Fatal(err)
	}
	got, err := c.get(t.Context(), request{detail: "a"}, constant("recomputed", 100))
	if err != nil || got != "a" {
		t.Errorf("recently used entry = %q, %v, want it kept", got, err)
	}
	got, err = c.get(t.Context(), request{detail: "b"}, constant("recomputed", 100))
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

	c := newTestCache(10)
	if _, err := c.get(t.Context(), request{detail: "big"}, constant("big", 1000)); err != nil {
		t.Fatal(err)
	}
	got, err := c.get(t.Context(), request{detail: "big"}, constant("recomputed", 1000))
	if err != nil || got != "big" {
		t.Errorf("get() = %q, %v, want the oversized result kept", got, err)
	}
	if _, err := c.get(t.Context(), request{detail: "next"}, constant("next", 1)); err != nil {
		t.Fatal(err)
	}
	got, err = c.get(t.Context(), request{detail: "big"}, constant("recomputed", 1))
	if err != nil || got != "recomputed" {
		t.Errorf("get() = %q, %v, want the oversized result evicted by the next one", got, err)
	}
}

func TestFailuresAreRetriedOnlyAfterTheirTTL(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		c := newTestCache(1 << 20)
		boom := errors.New("boom")
		var calls int
		compute := func(context.Context) (computed[string], error) {
			calls++
			return computed[string]{}, boom
		}
		for range 2 {
			if _, err := c.get(t.Context(), request{}, compute); !errors.Is(err, boom) {
				t.Fatalf("get() error = %v, want boom", err)
			}
		}
		if calls != 1 {
			t.Errorf("computed %d times inside the TTL, want once", calls)
		}
		time.Sleep(failureTTL)
		if _, err := c.get(t.Context(), request{}, compute); !errors.Is(err, boom) {
			t.Fatalf("get() error = %v, want boom", err)
		}
		if calls != 2 {
			t.Errorf("computed %d times after the TTL, want twice", calls)
		}
	})
}

func TestLastWaiterLeavingCancelsTheWork(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		c := newTestCache(1 << 20)
		var cancelled atomic.Bool
		block := func(ctx context.Context) (computed[string], error) {
			<-ctx.Done()
			cancelled.Store(true)
			return computed[string]{}, ctx.Err()
		}
		first, cancelFirst := context.WithCancel(t.Context())
		second, cancelSecond := context.WithCancel(t.Context())
		defer cancelSecond()
		var wg sync.WaitGroup
		for _, ctx := range []context.Context{first, second} {
			wg.Go(func() {
				if _, err := c.get(ctx, request{}, block); !errors.Is(err, context.Canceled) {
					t.Errorf("get() error = %v, want cancelled", err)
				}
			})
		}
		synctest.Wait()
		cancelFirst()
		synctest.Wait()
		if cancelled.Load() {
			t.Error("the work stopped while a waiter remained")
		}
		cancelSecond()
		wg.Wait()
		synctest.Wait()
		if !cancelled.Load() {
			t.Error("the work kept running with no waiters")
		}
		got, err := c.get(t.Context(), request{}, constant("fresh", 0))
		if err != nil || got != "fresh" {
			t.Errorf("get() after cancelling = %q, %v, want a fresh computation", got, err)
		}
	})
}

func TestWorkOutlivingTheTimeoutFails(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		c := newTestCache(1 << 20)
		_, err := c.get(t.Context(), request{}, func(ctx context.Context) (computed[string], error) {
			<-ctx.Done()
			return computed[string]{}, ctx.Err()
		})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("get() error = %v, want the deadline", err)
		}
	})
}

func TestGetRefusesACancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	c := newTestCache(1 << 20)
	_, err := c.get(ctx, request{}, func(context.Context) (computed[string], error) {
		t.Error("the work ran for a cancelled context")
		return computed[string]{}, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("get() error = %v, want cancelled", err)
	}
}

func TestInvalidateDropsOnlyThatDirectory(t *testing.T) {
	t.Parallel()

	c := newTestCache(1 << 20)
	for _, dir := range []string{"/a", "/b"} {
		if _, err := c.get(t.Context(), request{dir: dir}, constant("old", 1)); err != nil {
			t.Fatal(err)
		}
	}
	c.invalidate("/a")
	if got, _ := c.get(t.Context(), request{dir: "/a"}, constant("new", 1)); got != "new" {
		t.Errorf("/a = %q, want recomputed", got)
	}
	if got, _ := c.get(t.Context(), request{dir: "/b"}, constant("new", 1)); got != "old" {
		t.Errorf("/b = %q, want kept", got)
	}
}

func TestInvalidateDuringWorkDoesNotStoreItsResult(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		c := newTestCache(1 << 20)
		release := make(chan struct{})
		var wg sync.WaitGroup
		wg.Go(func() {
			got, err := c.get(t.Context(), request{dir: "/a"}, func(context.Context) (computed[string], error) {
				<-release
				return computed[string]{value: "old"}, nil
			})
			if err != nil || got != "old" {
				t.Errorf("waiter got %q, %v", got, err)
			}
		})
		synctest.Wait()
		c.invalidate("/a")
		close(release)
		wg.Wait()
		synctest.Wait()
		if got, _ := c.get(t.Context(), request{dir: "/a"}, constant("new", 1)); got != "new" {
			t.Errorf("get() = %q, want the invalidated result gone", got)
		}
	})
}
