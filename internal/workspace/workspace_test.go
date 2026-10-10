package workspace

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTestWorkspace(c *clock) *Workspace {
	ws := New(func(string) (*File, bool) { return nil, false }, func(string) *dialect.Dialect { return nil })
	ws.now = c.now
	return ws
}

func rewrite(t *testing.T, path, text string, at time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func firstFunction(t *testing.T, ws *Workspace, path string) string {
	t.Helper()
	f, err := ws.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return f.Tree.Functions[0].Name
}

func TestUnwatchedFilesRevalidateAfterTheRecheckInterval(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "a.sh")
	c := &clock{t: time.Now()}
	ws := newTestWorkspace(c)
	rewrite(t, path, "one() { :; }\n", c.t)
	if got := firstFunction(t, ws, path); got != "one" {
		t.Fatalf("first load = %s", got)
	}
	rewrite(t, path, "two() { :; }\n", c.t.Add(time.Hour))
	c.t = c.t.Add(DiskRecheck / 2)
	if got := firstFunction(t, ws, path); got != "one" {
		t.Errorf("load within the recheck interval = %s, want the cached one", got)
	}
	c.t = c.t.Add(DiskRecheck)
	if got := firstFunction(t, ws, path); got != "two" {
		t.Errorf("load after the recheck interval = %s, want the changed file", got)
	}
}

func TestWatchedEntriesSkipValidationUntilInvalidated(t *testing.T) {
	t.Parallel()

	root, outside := t.TempDir(), t.TempDir()
	inside, external := filepath.Join(root, "a.sh"), filepath.Join(outside, "b.sh")
	c := &clock{t: time.Now()}
	ws := newTestWorkspace(c)
	ws.SetRoots([]string{root})
	ws.SetWatched(true)
	rewrite(t, inside, "one() { :; }\n", c.t)
	rewrite(t, external, "one() { :; }\n", c.t)
	firstFunction(t, ws, inside)
	firstFunction(t, ws, external)
	rewrite(t, inside, "two() { :; }\n", c.t.Add(time.Hour))
	rewrite(t, external, "two() { :; }\n", c.t.Add(time.Hour))
	c.t = c.t.Add(time.Hour)
	if got := firstFunction(t, ws, inside); got != "one" {
		t.Errorf("watched file = %s, want the cached one until an event invalidates it", got)
	}
	if got := firstFunction(t, ws, external); got != "two" {
		t.Errorf("file outside the watched roots = %s, want it revalidated", got)
	}
	generation := ws.Generation()
	ws.Invalidate(inside)
	if ws.Generation() == generation {
		t.Error("Invalidate did not advance the generation")
	}
	if got := firstFunction(t, ws, inside); got != "two" {
		t.Errorf("watched file after invalidation = %s", got)
	}
}

func TestFileMemoFollowsGeneration(t *testing.T) {
	t.Parallel()

	f := &File{}
	calls := 0
	compute := func() any { calls++; return calls }
	first := f.Memo("k", 1, compute)
	again := f.Memo("k", 1, compute)
	next := f.Memo("k", 2, compute)
	if first != 1 || again != 1 || next != 2 {
		t.Errorf("memo results %v %v %v, want 1 1 2", first, again, next)
	}
}

func TestFileMemoComputesOnceForConcurrentCallers(t *testing.T) {
	t.Parallel()

	f := &File{}
	var calls atomic.Int32
	release := make(chan struct{})
	compute := func() any {
		calls.Add(1)
		<-release
		return "result"
	}
	var wg sync.WaitGroup
	results := make([]any, 8)
	for i := range results {
		wg.Go(func() { results[i] = f.Memo("k", 1, compute) })
	}
	for calls.Load() == 0 {
		runtime.Gosched()
	}
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Errorf("computed %d times, want once", calls.Load())
	}
	for i, r := range results {
		if r != "result" {
			t.Errorf("caller %d got %v", i, r)
		}
	}
}

func TestFileMemoRecomputesAfterAPanic(t *testing.T) {
	t.Parallel()

	f := &File{}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("the panic did not reach the computing caller")
			}
		}()
		f.Memo("k", 1, func() any { panic("boom") })
	}()
	if got := f.Memo("k", 1, func() any { return "recovered" }); got != "recovered" {
		t.Errorf("after a panic the memo returned %v, want a fresh computation", got)
	}
}

func TestShellFilesCacheFollowsDirectoryChanges(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, dir := range []string{"sub", "node_modules", "sub/deep"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"a.sh", "notes.txt", "node_modules/x.sh", "sub/deep/b.bash", "sub/.zshrc"} {
		if err := os.WriteFile(filepath.Join(root, f), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ws := New(func(string) (*File, bool) { return nil, false }, func(string) *dialect.Dialect { return nil })
	ws.SetRoots([]string{root})
	rel := func() []string {
		paths, err := ws.ShellFiles(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, len(paths))
		for i, p := range paths {
			out[i], _ = filepath.Rel(root, p)
		}
		return out
	}
	if got := rel(); len(got) != 3 || got[0] != "a.sh" || got[1] != "sub/.zshrc" || got[2] != "sub/deep/b.bash" {
		t.Fatalf("ShellFiles = %v", got)
	}
	if err := os.WriteFile(filepath.Join(root, "sub/deep/c.sh"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(root, "sub/deep"), later, later); err != nil {
		t.Fatal(err)
	}
	if got := rel(); len(got) != 4 || got[3] != "sub/deep/c.sh" {
		t.Errorf("ShellFiles after adding a file = %v", got)
	}
}
