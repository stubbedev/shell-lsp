package diskwatchtest

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/diskwatch"
)

func New(t testing.TB) *diskwatch.Watcher {
	t.Helper()
	w, err := diskwatch.New()
	if err != nil {
		t.Skipf("no inotify here: %v", err)
	}
	t.Cleanup(w.Close)
	return w
}

func Dir(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	probe := New(t)
	if !probe.Unchanged(probe.TrackDir(dir)) {
		t.Skipf("inotify cannot watch every ancestor of %s", dir)
	}
	return dir
}
