package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/parallel"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

const DiskRecheck = time.Second

const (
	maxFileSize  = 4 << 20
	maxScanFiles = 20000
	shebangProbe = 256
)

var shellExtensions = []string{".sh", ".bash", ".zsh", ".ksh", ".dash", ".bats"}

var skippedDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true, "node_modules": true, ".direnv": true, ".venv": true,
	"vendor": true, "__pycache__": true, ".cache": true, "result": true,
}

type File struct {
	Path string
	URI  string
	Doc  *docstore.Document
	Tree *parser.Tree

	memoMu sync.Mutex
	memo   map[string]*memoEntry
}

type memoEntry struct {
	generation uint64
	done       chan struct{}
	value      any
	ok         bool
}

func Memo[T any](f *File, key string, generation uint64, compute func() T) T {
	value, ok := f.memoize(key, generation, func() any { return compute() }).(T)
	if !ok {
		panic(fmt.Sprintf("workspace: memo %q of %s holds a value of another type", key, f.URI))
	}
	return value
}

func (f *File) memoize(key string, generation uint64, compute func() any) any {
	for {
		entry, owner := f.claim(key, generation)
		if owner {
			return f.fill(key, entry, compute)
		}
		<-entry.done
		if entry.ok {
			return entry.value
		}
	}
}

func (f *File) claim(key string, generation uint64) (*memoEntry, bool) {
	f.memoMu.Lock()
	defer f.memoMu.Unlock()
	if existing, ok := f.memo[key]; ok && existing.generation == generation {
		return existing, false
	}
	if f.memo == nil {
		f.memo = map[string]*memoEntry{}
	}
	fresh := &memoEntry{generation: generation, done: make(chan struct{})}
	f.memo[key] = fresh
	return fresh, true
}

func (f *File) fill(key string, entry *memoEntry, compute func() any) any {
	defer close(entry.done)
	defer func() {
		if entry.ok {
			return
		}
		f.memoMu.Lock()
		if f.memo[key] == entry {
			delete(f.memo, key)
		}
		f.memoMu.Unlock()
	}()
	entry.value = compute()
	entry.ok = true
	return entry.value
}

type Overlay func(uri string) (*File, bool)

type Workspace struct {
	overlay   Overlay
	dialectOf func(path string, in dialect.Input) dialect.Dialect
	now       func() time.Time

	mu         sync.Mutex
	roots      []string
	files      map[string]diskEntry
	dirs       map[string]dirListing
	generation atomic.Uint64
	watched    atomic.Bool
}

func (w *Workspace) SetWatched(watched bool) { w.watched.Store(watched) }

func isShellName(path string) bool {
	return dialect.IsRCFile(path) || slices.Contains(shellExtensions, filepath.Ext(path))
}

func Within(path, root string) bool {
	return under(filepath.Clean(path), filepath.Clean(root))
}

func under(path, root string) bool {
	rest, ok := strings.CutPrefix(path, root)
	return ok && (rest == "" || rest[0] == filepath.Separator || strings.HasSuffix(root, string(filepath.Separator)))
}

type diskEntry struct {
	size    int64
	modTime time.Time
	checked time.Time
	file    *File
}

func DetectDialect(_ string, in dialect.Input) dialect.Dialect {
	return dialect.Detect(nil, in)
}

func New(overlay Overlay, dialectOf func(path string, in dialect.Input) dialect.Dialect) *Workspace {
	return &Workspace{overlay: overlay, dialectOf: dialectOf, now: time.Now, files: map[string]diskEntry{}, dirs: map[string]dirListing{}}
}

func (w *Workspace) SetRoots(roots []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.roots = make([]string, len(roots))
	for i, root := range roots {
		w.roots[i] = filepath.Clean(root)
	}
	w.generation.Add(1)
}

func (w *Workspace) Generation() uint64 { return w.generation.Load() }

func (w *Workspace) Roots() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.roots)
}

func (w *Workspace) RootOf(path string) (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	best := ""
	for _, root := range w.roots {
		if Within(path, root) && len(root) > len(best) {
			best = root
		}
	}
	return best, best != ""
}

func (w *Workspace) Invalidate(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.files, path)
	delete(w.dirs, filepath.Dir(path))
	w.generation.Add(1)
}

func (w *Workspace) InvalidateAll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	clear(w.files)
	clear(w.dirs)
	w.generation.Add(1)
}

func (w *Workspace) Load(path string) (*File, error) {
	now := w.now()
	w.mu.Lock()
	cached, ok := w.files[path]
	fresh := ok && (w.watchedLocked(path) || now.Sub(cached.checked) < DiskRecheck)
	w.mu.Unlock()
	var uri string
	if ok {
		uri = cached.file.URI
	} else {
		uri = fileuri.FromPath(path)
	}
	if f, open := w.overlay(uri); open {
		return f, nil
	}
	if fresh {
		return cached.file, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", path, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("loading %s: %w", path, fs.ErrInvalid)
	}
	if info.Size() > maxFileSize {
		return nil, fmt.Errorf("loading %s: %d bytes exceeds the %d byte limit", path, info.Size(), maxFileSize)
	}
	if ok && cached.size == info.Size() && cached.modTime.Equal(info.ModTime()) {
		cached.checked = now
		w.mu.Lock()
		w.files[path] = cached
		w.mu.Unlock()
		return cached.file, nil
	}
	raw, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", path, err)
	}
	doc := docstore.NewDocument(uri, "", 0, string(raw))
	in := dialect.Input{URI: uri, Text: doc.Text()}
	d := w.dialectOf(path, in)
	file := &File{Path: path, URI: uri, Doc: doc, Tree: parser.ParseEdit(nil, doc.Text(), d, parser.Variant(d, in))}
	w.mu.Lock()
	w.files[path] = diskEntry{size: info.Size(), modTime: info.ModTime(), checked: now, file: file}
	w.mu.Unlock()
	return file, nil
}

func (w *Workspace) watchedLocked(path string) bool {
	return w.watched.Load() && isShellName(path) && slices.ContainsFunc(w.roots, func(root string) bool { return under(path, root) })
}

func (w *Workspace) LoadAll(ctx context.Context, paths []string, workers int, report func(done, total int)) ([]*File, error) {
	loaded := make([]*File, len(paths))
	var progress sync.Mutex
	finished := 0
	parallel.For(workers, len(paths), func(i int) {
		if ctx.Err() != nil {
			return
		}
		if file, err := w.Load(paths[i]); err == nil {
			loaded[i] = file
		}
		progress.Lock()
		finished++
		report(finished, len(paths))
		progress.Unlock()
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return slices.DeleteFunc(loaded, func(f *File) bool { return f == nil }), nil
}

type dirListing struct {
	modTime time.Time
	files   []string
	dirs    []string
}

func (w *Workspace) ShellFiles(ctx context.Context) ([]string, error) {
	var out []string
	for _, root := range w.Roots() {
		pending := []string{root}
		for len(pending) > 0 && len(out) < maxScanFiles {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			dir := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			listing, err := w.listDir(dir)
			if errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("scanning workspace root %s: %w", root, err)
			}
			out = append(out, listing.files...)
			pending = append(pending, listing.dirs...)
		}
	}
	slices.Sort(out)
	out = slices.Compact(out)
	return out[:min(len(out), maxScanFiles)], nil
}

func (w *Workspace) listDir(dir string) (dirListing, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return dirListing{}, err
	}
	w.mu.Lock()
	cached, ok := w.dirs[dir]
	w.mu.Unlock()
	if ok && cached.modTime.Equal(info.ModTime()) {
		return cached, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return dirListing{}, err
	}
	listing := dirListing{modTime: info.ModTime()}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		switch {
		case entry.IsDir():
			if !skippedDirs[entry.Name()] {
				listing.dirs = append(listing.dirs, path)
			}
		case entry.Type().IsRegular() && isShellFile(path, entry):
			listing.files = append(listing.files, path)
		}
	}
	w.mu.Lock()
	w.dirs[dir] = listing
	w.mu.Unlock()
	return listing, nil
}

func WatchGlobs() []string {
	extensions := make([]string, len(shellExtensions))
	for i, ext := range shellExtensions {
		extensions[i] = strings.TrimPrefix(ext, ".")
	}
	names := dialect.RCFileNames()
	for i, name := range names {
		names[i] = strings.TrimPrefix(name, ".")
	}
	return []string{
		"**/*.{" + strings.Join(extensions, ",") + "}",
		"**/.{" + strings.Join(names, ",") + "}",
	}
}

func isShellFile(path string, entry fs.DirEntry) bool {
	if isShellName(path) {
		return true
	}
	if filepath.Ext(path) != "" {
		return false
	}
	info, err := entry.Info()
	if err != nil || info.Mode().Perm()&0o111 == 0 {
		return false
	}
	return hasShellShebang(path)
}

func hasShellShebang(path string) bool {
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, shebangProbe)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return false
	}
	line, _, _ := bytes.Cut(head[:n], []byte("\n"))
	_, ok := dialect.FromShebang(string(line))
	return ok
}
