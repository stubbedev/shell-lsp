//go:build linux

package diskwatch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestWatcher(t *testing.T) *Watcher {
	t.Helper()
	w, err := New()
	if err != nil {
		t.Skipf("no inotify here: %v", err)
	}
	t.Cleanup(w.Close)
	return w
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestChangesToATrackedFileAreSeen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.nix")
	write(t, path, "1")
	w := newTestWatcher(t)
	stamp := w.Track(path)
	w.Drain()
	if !w.Unchanged(stamp) {
		t.Fatal("a quiet file reads as changed")
	}
	write(t, path, "2")
	w.Drain()
	if w.Unchanged(stamp) {
		t.Fatal("an edit to a tracked file went unseen")
	}
}

func TestAMissingFileIsSeenWhenItAppears(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing", "deep", ".shellcheckrc")
	w := newTestWatcher(t)
	stamp := w.Track(path)
	w.Drain()
	if !w.Unchanged(stamp) {
		t.Fatal("nothing changed yet")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	w.Drain()
	if w.Unchanged(stamp) {
		t.Fatal("the appearing parent directory went unseen")
	}
	stamp = w.Track(path)
	write(t, path, "x")
	w.Drain()
	if w.Unchanged(stamp) {
		t.Fatal("the appearing file went unseen")
	}
}

func TestUnrelatedNamesInAWatchedDirectoryAreIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.nix")
	write(t, path, "1")
	w := newTestWatcher(t)
	stamp := w.Track(path)
	write(t, filepath.Join(dir, "scratch.tmp"), "noise")
	w.Drain()
	if !w.Unchanged(stamp) {
		t.Fatal("a file nothing tracks should not disturb the stamp")
	}
}

func TestAListingSeesOnlyLinkChanges(t *testing.T) {
	dir := t.TempDir()
	child := filepath.Join(dir, "a.nix")
	write(t, child, "1")
	w := newTestWatcher(t)
	stamp := w.TrackDir(dir)
	write(t, child, "2")
	w.Drain()
	if !w.Unchanged(stamp) {
		t.Fatal("rewriting a file leaves the listing as it was")
	}
	write(t, filepath.Join(dir, "b.nix"), "1")
	w.Drain()
	if w.Unchanged(stamp) {
		t.Fatal("a new file went unseen by the listing")
	}
}

func TestRenamingAnAncestorIsSeen(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "f.nix")
	write(t, path, "1")
	w := newTestWatcher(t)
	stamp := w.Track(path)
	if err := os.Rename(filepath.Join(root, "a"), filepath.Join(root, "moved")); err != nil {
		t.Fatal(err)
	}
	w.Drain()
	if w.Unchanged(stamp) {
		t.Fatal("moving an ancestor directory went unseen")
	}
	moved := filepath.Join(root, "moved", "b", "f.nix")
	fresh := w.Track(moved)
	w.Drain()
	w.Drain()
	if !w.Unchanged(fresh) {
		t.Fatal("the watches removed by the move must not read as new losses")
	}
	write(t, moved, "2")
	w.Drain()
	if w.Unchanged(fresh) {
		t.Fatal("tracking again after a move does not work")
	}
}

func TestSymlinkedFilesFollowTheirTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real", "config")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, target, "1")
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	w := newTestWatcher(t)
	stamp := w.Track(link)
	write(t, target, "2")
	w.Drain()
	if w.Unchanged(stamp) {
		t.Fatal("an edit behind a symlink went unseen")
	}
}

func TestABusyWriterCannotHoldUpADrain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.nix")
	write(t, path, "1")
	w := newTestWatcher(t)
	w.Track(path)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		f, err := os.Create(filepath.Join(dir, "busy.log"))
		if err != nil {
			t.Error(err)
			return
		}
		defer f.Close()
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = f.WriteString("x")
			}
		}
	}()
	for range 100 {
		finished := make(chan struct{})
		go func() {
			w.Drain()
			close(finished)
		}()
		select {
		case <-finished:
		case <-time.After(10 * time.Second):
			t.Fatal("a drain never finished while a file kept changing")
		}
	}
	close(stop)
	<-done
}

func TestANilWatcherTracksNothing(t *testing.T) {
	var w *Watcher
	w.Drain()
	w.Close()
	if w.Unchanged(w.Track("/x")) {
		t.Fatal("a nil watcher vouched for a file")
	}
}

func TestAnOverflowForgetsEverything(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a")
	write(t, path, "1")
	w := newTestWatcher(t)
	stamp := w.Track(path)
	w.mu.Lock()
	w.handle(kernelEvent{wd: -1, mask: eventOverflow})
	w.mu.Unlock()
	if w.Unchanged(stamp) {
		t.Fatal("a lost event queue must invalidate every stamp")
	}
}
