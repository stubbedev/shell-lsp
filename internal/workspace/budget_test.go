package workspace

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/regularfile"
)

func noOverlay(string) (*File, bool) { return nil, false }

func writeNix(t *testing.T, dir, name string, size int) string {
	t.Helper()
	path := filepath.Join(dir, name)
	text := "# " + strings.Repeat("x", max(size-len("# \n{ }\n"), 0)) + "\n{ }\n"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAScanThatOutgrowsTheStashHoldsNoParse(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	w := New(noOverlay, Detect)
	for i := range 4 {
		path := writeNix(t, dir, fmt.Sprintf("big%d.nix", i), maxStashedBytes*2/3)
		if _, _, err := w.NixIncludes(path); err != nil {
			t.Fatal(err)
		}
	}
	if w.stashed != 0 || len(w.stash) != 0 {
		t.Errorf("a workspace past the budget keeps %d bytes in %d parses", w.stashed, len(w.stash))
	}
	w.ReleaseStash()
	small := writeNix(t, dir, "small.nix", 64)
	if _, _, err := w.NixIncludes(small); err != nil {
		t.Fatal(err)
	}
	if w.stashed == 0 {
		t.Error("the next scan keeps nothing, so a small project parses twice")
	}
}

func TestAScanWithinTheStashKeepsItsParses(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	w := New(noOverlay, Detect)
	for i := range 3 {
		if _, _, err := w.NixIncludes(writeNix(t, dir, fmt.Sprintf("f%d.nix", i), 1024)); err != nil {
			t.Fatal(err)
		}
	}
	if len(w.stash) != 3 {
		t.Errorf("%d parses kept, want every file of a small project", len(w.stash))
	}
}

func TestBatchesAreBoundedByFilesAndBytes(t *testing.T) {
	t.Parallel()

	sizes := map[string]int64{}
	var paths []string
	add := func(n int, size int64) {
		for range n {
			path := fmt.Sprintf("/w/%06d.nix", len(paths))
			paths = append(paths, path)
			sizes[path] = size
		}
	}
	add(maxBatchFiles*2+7, 10)
	add(5, maxBatchBytes/2+1)
	add(1, 2*maxBatchBytes)
	add(3, 10)
	w := New(noOverlay, Detect)
	w.UseStat(func(path string) (fs.FileInfo, error) { return sizedInfo(sizes[path]), nil })

	var joined []string
	for batch := range w.Batches(paths) {
		if len(batch) > maxBatchFiles {
			t.Errorf("a batch of %d files", len(batch))
		}
		var total int64
		for _, path := range batch {
			total += sizes[path]
		}
		if total > maxBatchBytes && len(batch) > 1 {
			t.Errorf("a batch of %d files holds %d bytes", len(batch), total)
		}
		joined = append(joined, batch...)
	}
	if !slices.Equal(joined, paths) {
		t.Errorf("the batches cover %d of %d paths or change their order", len(joined), len(paths))
	}
}

func TestParsingImportsIsBoundedByBytes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	w := New(noOverlay, Detect)
	var inFlight, highest atomic.Int64
	w.UseRead(func(path string, info fs.FileInfo, limit int64) ([]byte, fs.FileInfo, error) {
		now := inFlight.Add(info.Size())
		defer inFlight.Add(-info.Size())
		for {
			seen := highest.Load()
			if now <= seen || highest.CompareAndSwap(seen, now) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		return regularfile.ReadStat(path, info, limit)
	})
	var wg sync.WaitGroup
	for i := range 8 {
		path := writeNix(t, dir, fmt.Sprintf("f%d.nix", i), maxParsingBytes/2)
		wg.Go(func() {
			if _, _, err := w.NixIncludes(path); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if got := highest.Load(); got > maxParsingBytes {
		t.Errorf("%d bytes were parsed at once, the budget is %d", got, maxParsingBytes)
	}
}

type sizedInfo int64

func (s sizedInfo) Name() string     { return "" }
func (s sizedInfo) Size() int64      { return int64(s) }
func (sizedInfo) Mode() fs.FileMode  { return 0 }
func (sizedInfo) ModTime() time.Time { return time.Time{} }
func (sizedInfo) IsDir() bool        { return false }
func (sizedInfo) Sys() any           { return nil }
