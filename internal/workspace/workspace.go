package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"math"
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
	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/parallel"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/platform"
	"github.com/stubbedev/shell-lsp/internal/regularfile"
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
	overlay Overlay
	resolve func(path string, in dialect.Input) dialect.Detection
	now     func() time.Time

	mu         sync.Mutex
	roots      []string
	files      map[string]diskEntry
	dirs       map[string]dirListing
	missing    map[string]time.Time
	generation atomic.Uint64
	watched    atomic.Bool
}

func (w *Workspace) SetWatched(watched bool) { w.watched.Store(watched) }

func isShellName(path string) bool {
	if _, ok := host.FromName(path); ok {
		return true
	}
	return dialect.IsRCFile(path) || slices.Contains(shellExtensions, filepath.Ext(path))
}

type diskEntry struct {
	size    int64
	modTime time.Time
	checked time.Time
	file    *File
}

func Detect(_ string, in dialect.Input) dialect.Detection {
	return dialect.Resolve(nil, in)
}

func New(overlay Overlay, resolve func(path string, in dialect.Input) dialect.Detection) *Workspace {
	return &Workspace{overlay: overlay, resolve: resolve, now: time.Now, files: map[string]diskEntry{}, dirs: map[string]dirListing{}, missing: map[string]time.Time{}}
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
	return InnermostRoot(path, slices.Values(w.roots))
}

func InnermostRoot(path string, roots iter.Seq[string]) (string, bool) {
	best := ""
	for root := range roots {
		if platform.Native.Within(path, root) && len(root) > len(best) {
			best = root
		}
	}
	return best, best != ""
}

func (w *Workspace) Invalidate(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	key := platform.Native.Fold(path)
	delete(w.files, key)
	delete(w.missing, key)
	delete(w.dirs, platform.Native.Fold(filepath.Dir(path)))
	w.generation.Add(1)
}

func (w *Workspace) InvalidateAll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	clear(w.files)
	clear(w.dirs)
	clear(w.missing)
	w.generation.Add(1)
}

func (w *Workspace) Load(path string) (*File, error) {
	now := w.now()
	key := platform.Native.Fold(path)
	w.mu.Lock()
	cached, ok := w.files[key]
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
	w.mu.Lock()
	missingSince, missing := w.missing[key]
	w.mu.Unlock()
	if missing && now.Sub(missingSince) < DiskRecheck {
		return nil, fmt.Errorf("loading %s: %w", path, fs.ErrNotExist)
	}
	info, err := regularfile.Stat(path)
	if err != nil {
		w.mu.Lock()
		delete(w.files, key)
		if errors.Is(err, fs.ErrNotExist) {
			w.rememberMissingLocked(key, now)
		}
		w.mu.Unlock()
		return nil, fmt.Errorf("loading %s: %w", path, err)
	}
	if ok && cached.size == info.Size() && cached.modTime.Equal(info.ModTime()) {
		cached.checked = now
		w.mu.Lock()
		w.files[key] = cached
		w.mu.Unlock()
		return cached.file, nil
	}
	raw, info, err := regularfile.ReadStat(path, info, maxFileSize)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", path, err)
	}
	file, _ := Parse(path, docstore.NewDocument(uri, "", 0, string(raw)), nil, func(in dialect.Input) dialect.Detection { return w.resolve(path, in) })
	w.mu.Lock()
	w.files[key] = diskEntry{size: info.Size(), modTime: info.ModTime(), checked: now, file: file}
	w.mu.Unlock()
	return file, nil
}

func Parse(path string, doc *docstore.Document, previous *parser.Tree, resolve func(dialect.Input) dialect.Detection) (*File, dialect.Detection) {
	if language, ok := host.Of(doc.URI, doc.Language); ok {
		script := language.Project(doc)
		return &File{Path: path, URI: doc.URI, Doc: script.Doc, Tree: script.Parse(previous)}, script.Detection
	}
	detection := resolve(dialect.Input{URI: doc.URI, LanguageID: doc.Language, Text: doc.Text()})
	return &File{Path: path, URI: doc.URI, Doc: doc, Tree: parser.ParseEdit(previous, doc.Text(), detection.Shell)}, detection
}

const maxMissing = 4096

func (w *Workspace) rememberMissingLocked(key string, now time.Time) {
	if len(w.missing) >= maxMissing {
		for missingKey, since := range w.missing {
			if now.Sub(since) >= DiskRecheck {
				delete(w.missing, missingKey)
			}
		}
		if len(w.missing) >= maxMissing {
			clear(w.missing)
		}
	}
	w.missing[key] = now
}

func ReadDisk(path string) ([]byte, fs.FileInfo, error) {
	return regularfile.Read(path, maxFileSize)
}

func (w *Workspace) watchedLocked(path string) bool {
	return w.watched.Load() && isShellName(path) && slices.ContainsFunc(w.roots, func(root string) bool { return platform.Native.Within(path, root) })
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
	return w.scan(ctx, maxScanFiles)
}

func (w *Workspace) AllShellFiles(ctx context.Context) ([]string, error) {
	return w.scan(ctx, math.MaxInt)
}

func (w *Workspace) scan(ctx context.Context, limit int) ([]string, error) {
	var out []string
	for _, root := range w.Roots() {
		pending := []string{root}
		for len(pending) > 0 && len(out) < limit {
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
	return out[:min(len(out), limit)], nil
}

func (w *Workspace) listDir(dir string) (dirListing, error) {
	info, err := os.Stat(dir)
	if err != nil {
		w.mu.Lock()
		w.forgetTreeLocked(dir)
		w.mu.Unlock()
		return dirListing{}, err
	}
	w.mu.Lock()
	cached, ok := w.dirs[platform.Native.Fold(dir)]
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
	if previous, ok := w.dirs[platform.Native.Fold(dir)]; ok {
		w.forgetRemovedLocked(previous, listing)
	}
	w.dirs[platform.Native.Fold(dir)] = listing
	w.mu.Unlock()
	return listing, nil
}

func (w *Workspace) forgetRemovedLocked(previous, current dirListing) {
	for _, file := range previous.files {
		if _, kept := slices.BinarySearch(current.files, file); !kept {
			delete(w.files, platform.Native.Fold(file))
		}
	}
	for _, dir := range previous.dirs {
		if _, kept := slices.BinarySearch(current.dirs, dir); !kept {
			w.forgetTreeLocked(dir)
		}
	}
}

func (w *Workspace) forgetTreeLocked(dir string) {
	for key := range w.dirs {
		if platform.Native.Within(key, dir) {
			delete(w.dirs, key)
		}
	}
	for key := range w.files {
		if platform.Native.Within(key, dir) {
			delete(w.files, key)
		}
	}
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
	for _, ext := range host.Extensions() {
		extensions = append(extensions, strings.TrimPrefix(ext, "."))
	}
	return []string{
		"**/*.{" + strings.Join(extensions, ",") + "}",
		"**/.{" + strings.Join(names, ",") + "}",
		"**/{" + strings.Join(host.Names(), ",") + "}",
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
	if err != nil || !platform.Native.MayExecute(info.Mode()) {
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
