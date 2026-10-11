package server

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/docstore"
)

func alwaysOpen(string) bool { return true }

func TestSnapshotCacheBuildsEachVersionOnce(t *testing.T) {
	t.Parallel()

	cache := newSnapshotCache(alwaysOpen)
	doc := docstore.NewDocument("file:///a.sh", "", 1, "echo hi\n")
	var builds atomic.Int32
	build := func(*snapshot) *snapshot {
		builds.Add(1)
		time.Sleep(20 * time.Millisecond)
		return &snapshot{doc: doc}
	}
	var wg sync.WaitGroup
	results := make([]*snapshot, 8)
	for i := range results {
		wg.Go(func() { results[i] = cache.get("file:///a.sh", doc, 0, build) })
	}
	wg.Wait()
	if n := builds.Load(); n != 1 {
		t.Errorf("built %d times, want once", n)
	}
	for _, r := range results {
		if r != results[0] {
			t.Fatal("callers received different snapshots for one version")
		}
	}
	newer := docstore.NewDocument("file:///a.sh", "", 2, "echo bye\n")
	cache.get("file:///a.sh", newer, 0, func(*snapshot) *snapshot { return &snapshot{doc: newer} })
	old := cache.get("file:///a.sh", doc, 0, func(*snapshot) *snapshot { builds.Add(1); return &snapshot{doc: doc} })
	if old.doc != doc || builds.Load() != 2 {
		t.Errorf("an older version must be rebuilt without displacing the newer entry")
	}
	if got := cache.get("file:///a.sh", newer, 0, func(*snapshot) *snapshot { t.Error("newer version rebuilt"); return nil }); got.doc != newer {
		t.Error("newer entry lost")
	}
}

func TestSnapshotCacheRebuildsAfterAPanickedBuild(t *testing.T) {
	t.Parallel()

	cache := newSnapshotCache(alwaysOpen)
	doc := docstore.NewDocument("file:///a.sh", "", 1, "echo hi\n")
	started, release := make(chan struct{}), make(chan struct{})
	waiter := make(chan *snapshot)
	go func() {
		defer func() { _ = recover() }()
		cache.get("file:///a.sh", doc, 0, func(*snapshot) *snapshot {
			close(started)
			<-release
			panic("parser bug")
		})
	}()
	<-started
	go func() {
		waiter <- cache.get("file:///a.sh", doc, 0, func(*snapshot) *snapshot { return &snapshot{doc: doc} })
	}()
	close(release)
	select {
	case got := <-waiter:
		if got == nil || got.doc != doc {
			t.Errorf("waiter got %+v, want a rebuilt snapshot", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a waiter hung after the build it waited on panicked")
	}
}

func TestSnapshotCacheHandsBuildsTheReadyPreviousVersion(t *testing.T) {
	t.Parallel()

	cache := newSnapshotCache(alwaysOpen)
	v1 := docstore.NewDocument("file:///a.sh", "", 1, "echo one\n")
	v2 := docstore.NewDocument("file:///a.sh", "", 2, "echo two\n")
	first := cache.get("file:///a.sh", v1, 0, func(previous *snapshot) *snapshot {
		if previous != nil {
			t.Errorf("first build got previous %+v", previous)
		}
		return &snapshot{doc: v1}
	})
	cache.get("file:///a.sh", v2, 0, func(previous *snapshot) *snapshot {
		if previous != first {
			t.Errorf("second build got previous %+v, want the first snapshot", previous)
		}
		return &snapshot{doc: v2}
	})

	building := docstore.NewDocument("file:///b.sh", "", 1, "echo b\n")
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		cache.get("file:///b.sh", building, 0, func(*snapshot) *snapshot {
			close(started)
			<-release
			return &snapshot{doc: building}
		})
	}()
	<-started
	next := docstore.NewDocument("file:///b.sh", "", 2, "echo c\n")
	cache.get("file:///b.sh", next, 0, func(previous *snapshot) *snapshot {
		if previous != nil {
			t.Error("a build was handed a previous snapshot that was still being built")
		}
		return &snapshot{doc: next}
	})
	close(release)
	<-done
}
