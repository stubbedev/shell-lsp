//go:build linux

package workspace

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/diskwatch"
	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/regularfile"
)

type watchedWorkspace struct {
	*Workspace
	disk  *diskwatch.Watcher
	clock *clock
	stats *atomic.Int64
}

func newWatchedWorkspace(t *testing.T) watchedWorkspace {
	t.Helper()
	disk, err := diskwatch.New()
	if err != nil {
		t.Skipf("no inotify here: %v", err)
	}
	t.Cleanup(disk.Close)
	c := &clock{t: time.Now()}
	ws := newTestWorkspace(c)
	ws.WatchDisk(disk)
	stats := &atomic.Int64{}
	ws.UseStat(func(path string) (fs.FileInfo, error) {
		stats.Add(1)
		return regularfile.Stat(path)
	})
	return watchedWorkspace{Workspace: ws, disk: disk, clock: c, stats: stats}
}

func (w watchedWorkspace) pastTheRecheck() { w.clock.t = w.clock.t.Add(2 * DiskRecheck) }

func TestAWatchedFileNeedsNoStatUntilItChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.sh")
	rewrite(t, path, "one() { :; }\n", time.Now())
	w := newWatchedWorkspace(t)
	if got := firstFunction(t, w.Workspace, path); got != "one" {
		t.Fatalf("first load = %q", got)
	}
	w.stats.Store(0)
	w.pastTheRecheck()
	w.disk.Drain()
	if got := firstFunction(t, w.Workspace, path); got != "one" {
		t.Fatalf("second load = %q", got)
	}
	if n := w.stats.Load(); n != 0 {
		t.Errorf("%d stats of a quiet file, want none", n)
	}
	rewrite(t, path, "two() { :; }\n", time.Now().Add(time.Hour))
	w.pastTheRecheck()
	w.disk.Drain()
	if got := firstFunction(t, w.Workspace, path); got != "two" {
		t.Errorf("after an edit outside the editor, first function = %q", got)
	}
}

func TestAWatchedMissingFileIsSeenWhenCreated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "later.sh")
	w := newWatchedWorkspace(t)
	if _, err := w.Load(path); err == nil {
		t.Fatal("loaded a file that does not exist")
	}
	w.stats.Store(0)
	w.pastTheRecheck()
	w.disk.Drain()
	if _, err := w.Load(path); err == nil {
		t.Fatal("loaded a file that does not exist")
	}
	if n := w.stats.Load(); n != 0 {
		t.Errorf("%d stats of a file known to be absent, want none", n)
	}
	rewrite(t, path, "made() { :; }\n", time.Now())
	w.pastTheRecheck()
	w.disk.Drain()
	if got := firstFunction(t, w.Workspace, path); got != "made" {
		t.Errorf("after creating it, first function = %q", got)
	}
}

func TestAWatchedScanSeesNewFilesAndIgnoreRules(t *testing.T) {
	root := t.TempDir()
	rewrite(t, filepath.Join(root, "a.sh"), "a() { :; }\n", time.Now())
	w := newWatchedWorkspace(t)
	w.SetRoots([]string{root})
	scan := func() []string {
		t.Helper()
		w.pastTheRecheck()
		w.disk.Drain()
		paths, err := w.ShellFiles(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		return paths
	}
	if got := scan(); !slices.Equal(got, []string{filepath.Join(root, "a.sh")}) {
		t.Fatalf("first scan = %v", got)
	}
	rewrite(t, filepath.Join(root, "b.sh"), "b() { :; }\n", time.Now())
	if got := scan(); !slices.Equal(got, []string{filepath.Join(root, "a.sh"), filepath.Join(root, "b.sh")}) {
		t.Errorf("after adding a file, scan = %v", got)
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := scan(); len(got) != 2 {
		t.Errorf("after creating a repository, scan = %v", got)
	}
	rewrite(t, filepath.Join(root, ".gitignore"), "b.sh\n", time.Now())
	if got := scan(); !slices.Equal(got, []string{filepath.Join(root, "a.sh")}) {
		t.Errorf("after ignoring b.sh, scan = %v", got)
	}
	rewrite(t, filepath.Join(root, ".gitignore"), "a.sh\n", time.Now().Add(time.Hour))
	if got := scan(); !slices.Equal(got, []string{filepath.Join(root, "b.sh")}) {
		t.Errorf("after changing the ignore rules, scan = %v", got)
	}
}

func TestWatchedNixIncludesFollowAnEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.nix")
	rewrite(t, path, "{ imports = [ ./one.nix ]; }\n", time.Now())
	w := newWatchedWorkspace(t)
	first, _, err := w.NixIncludes(path)
	if err != nil || len(first) != 1 {
		t.Fatalf("includes = %v, %v", first, err)
	}
	w.stats.Store(0)
	w.disk.Drain()
	if _, _, err := w.NixIncludes(path); err != nil {
		t.Fatal(err)
	}
	if n := w.stats.Load(); n != 0 {
		t.Errorf("%d stats of quiet includes, want none", n)
	}
	rewrite(t, path, "{ imports = [ ./one.nix ./two.nix ]; }\n", time.Now().Add(time.Hour))
	w.disk.Drain()
	second, _, err := w.NixIncludes(path)
	if err != nil || len(second) != 2 {
		t.Errorf("after an edit, includes = %v, %v", second, err)
	}
}

func TestANixFileIsReadOnceBetweenItsImportsAndItsLoad(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "a.nix")
	const text = "{ imports = [ ./b.nix ]; }\n"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	w := New(func(string) (*File, bool) { return nil, false }, Detect)
	reads := 0
	w.UseRead(func(name string, info fs.FileInfo, limit int64) ([]byte, fs.FileInfo, error) {
		reads++
		return regularfile.ReadStat(name, info, limit)
	})
	includes, _, err := w.NixIncludes(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(includes) != 1 {
		t.Fatalf("includes = %+v", includes)
	}
	file, err := w.LoadAs(path, host.Nix)
	if err != nil {
		t.Fatal(err)
	}
	if reads != 1 {
		t.Errorf("%d reads of one file, want one", reads)
	}
	if got := file.Doc.Source().Text(); got != text {
		t.Errorf("text = %q", got)
	}
	if len(file.Native.Index.Includes) == 0 {
		t.Error("the load lost what the file imports")
	}
	if w.stashed != 0 || len(w.stash) != 0 {
		t.Errorf("the parse stays held: %d bytes in %d entries", w.stashed, len(w.stash))
	}
}

func TestAKeptParseIsNotUsedForAFileThatChanged(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "a.nix")
	if err := os.WriteFile(path, []byte("{ a = 1; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := New(func(string) (*File, bool) { return nil, false }, Detect)
	if _, _, err := w.NixIncludes(path); err != nil {
		t.Fatal(err)
	}
	const changed = "{ a = 1; b = 2; }\n"
	if err := os.WriteFile(path, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := w.LoadAs(path, host.Nix)
	if err != nil {
		t.Fatal(err)
	}
	if got := file.Doc.Source().Text(); got != changed {
		t.Errorf("text = %q, want the file as it is now", got)
	}
}

func TestReleasingTheStashDropsWhatWasNeverLoaded(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "a.nix")
	if err := os.WriteFile(path, []byte("{ a = 1; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := New(func(string) (*File, bool) { return nil, false }, Detect)
	if _, _, err := w.NixIncludes(path); err != nil {
		t.Fatal(err)
	}
	if w.stashed == 0 {
		t.Fatal("nothing was kept")
	}
	w.ReleaseStash()
	if w.stashed != 0 || w.stash != nil {
		t.Errorf("%d bytes stay held", w.stashed)
	}
}
