package workspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

func TestWatchedEntriesSkipValidationUntilInvalidated(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "a.sh")
	if err := os.WriteFile(path, []byte("one() { :; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ws := New(func(string) (*File, bool) { return nil, false }, func(string) *dialect.Dialect { return nil })
	first, err := ws.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("two() { :; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	unwatched, err := ws.Load(path)
	if err != nil || unwatched == first {
		t.Fatalf("without watchers a changed file must be reloaded: %v", err)
	}
	ws.SetWatched(true)
	if err := os.WriteFile(path, []byte("three() { :; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if trusted, _ := ws.Load(path); trusted != unwatched {
		t.Error("watched entry was revalidated instead of trusted")
	}
	generation := ws.Generation()
	ws.Invalidate(path)
	if ws.Generation() == generation {
		t.Error("Invalidate did not advance the generation")
	}
	fresh, err := ws.Load(path)
	if err != nil || fresh.Tree.Functions[0].Name != "three" {
		t.Errorf("after invalidation = %+v, %v", fresh, err)
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
