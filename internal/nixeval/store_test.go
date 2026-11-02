package nixeval

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T, budget int64) (*store, *[]error) {
	t.Helper()
	reports := &[]error{}
	return &store{dir: t.TempDir(), budget: budget, now: time.Now, report: func(err error) { *reports = append(*reports, err) }}, reports
}

func TestStoreRoundTripsAndRefusesAnotherKey(t *testing.T) {
	t.Parallel()

	s, reports := newTestStore(t, 1<<20)
	key := testKey("a")
	s.write(key, []byte("payload"))
	got, ok := s.read(key)
	if !ok || string(got) != "payload" {
		t.Errorf("read() = %q, %v", got, ok)
	}
	if _, ok := s.read(testKey("absent")); ok {
		t.Error("read() found an absent key")
	}
	if len(*reports) != 0 {
		t.Errorf("reported %v", *reports)
	}
	moved := testKey("moved")
	if err := os.Rename(s.entryPath(key), s.entryPath(moved)); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.read(moved); ok {
		t.Error("read() trusted an entry written for another key")
	}
	if len(*reports) != 1 {
		t.Errorf("reports = %v, want the foreign entry", *reports)
	}
}

func TestStoreEvictsTheOldestPastItsBudgetAndKeepsTheNewest(t *testing.T) {
	t.Parallel()

	payload := make([]byte, 100)
	entrySize := int64(len(frame(testKey("x"), payload)))
	s, reports := newTestStore(t, 2*entrySize)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, name := range []string{"a", "b", "c"} {
		s.write(testKey(name), payload)
		at := base.Add(time.Duration(i) * time.Hour)
		if err := os.Chtimes(s.entryPath(testKey(name)), at, at); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := s.read(testKey("a")); ok {
		t.Error("the oldest entry survived")
	}
	for _, name := range []string{"b", "c"} {
		if _, ok := s.read(testKey(name)); !ok {
			t.Errorf("entry %s was evicted", name)
		}
	}
	oversized := make([]byte, 10*entrySize)
	s.write(testKey("big"), oversized)
	if _, ok := s.read(testKey("big")); !ok {
		t.Error("the entry just written was evicted")
	}
	listed, err := filepath.Glob(filepath.Join(s.dir, "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Errorf("%d files remain, want only the oversized one", len(listed))
	}
	if len(*reports) != 0 {
		t.Errorf("reported %v", *reports)
	}
}

func TestStoreLatestPointerRejectsDamage(t *testing.T) {
	t.Parallel()

	s, reports := newTestStore(t, 1<<20)
	slot := slotKey(testKey("slot"))
	if _, ok := s.latest(slot); ok {
		t.Error("latest() found an absent pointer")
	}
	s.setLatest(slot, testKey("a"))
	if got, ok := s.latest(slot); !ok || got != testKey("a") {
		t.Errorf("latest() = %x, %v", got, ok)
	}
	writeFile(t, s.latestPath(slot), "short")
	if _, ok := s.latest(slot); ok {
		t.Error("latest() trusted a damaged pointer")
	}
	if len(*reports) != 1 {
		t.Errorf("reports = %v, want the damaged pointer", *reports)
	}
}
