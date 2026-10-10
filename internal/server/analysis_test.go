package server

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/docstore"
)

func TestSnapshotCacheBuildsEachVersionOnce(t *testing.T) {
	t.Parallel()

	cache := newSnapshotCache()
	doc := docstore.NewDocument("file:///a.sh", "", 1, "echo hi\n")
	var builds atomic.Int32
	build := func() *snapshot {
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
	cache.get("file:///a.sh", newer, 0, func() *snapshot { return &snapshot{doc: newer} })
	old := cache.get("file:///a.sh", doc, 0, func() *snapshot { builds.Add(1); return &snapshot{doc: doc} })
	if old.doc != doc || builds.Load() != 2 {
		t.Errorf("an older version must be rebuilt without displacing the newer entry")
	}
	if got := cache.get("file:///a.sh", newer, 0, func() *snapshot { t.Error("newer version rebuilt"); return nil }); got.doc != newer {
		t.Error("newer entry lost")
	}
}
