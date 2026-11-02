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
	"weak"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/gitignore"
	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/host/model"
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
	ignoreFile   = ".gitignore"
	nixExtension = ".nix"
)

var shellExtensions = []string{".sh", ".bash", ".zsh", ".ksh", ".dash", ".bats"}

var skippedDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true, "node_modules": true, ".direnv": true, ".venv": true,
	"vendor": true, "__pycache__": true, ".cache": true, "result": true,
}

type File struct {
	Path      string
	URI       string
	Doc       *docstore.Document
	Tree      *parser.Tree
	Detection dialect.Detection
	Native    *host.Native
	Others    []*File
	spans     []parser.Span

	memoMu sync.Mutex
	memo   map[string]*memoEntry
}

func (f *File) At(offset int) *File {
	for _, other := range f.Others {
		if slices.ContainsFunc(other.spans, func(s parser.Span) bool { return s.Start <= offset && offset < s.End }) {
			return other
		}
	}
	return f
}

func (f *File) Variants() []*File { return append([]*File{f}, f.Others...) }

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
	ignores    map[string]ignoreRules
	missing    map[string]time.Time
	includes   map[string]includesEntry
	generation atomic.Uint64
	watched    atomic.Bool
	scanCut    atomic.Bool
}

func (w *Workspace) SetWatched(watched bool) { w.watched.Store(watched) }

func (w *Workspace) Watched() bool { return w.watched.Load() }

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
	held    *File
	dropped weak.Pointer[File]
}

func newDiskEntry(file *File, info fs.FileInfo, checked time.Time) diskEntry {
	entry := diskEntry{size: info.Size(), modTime: info.ModTime(), checked: checked}
	if file.Native != nil && file.Native.Language == host.Nix {
		entry.dropped = weak.Make(file)
		return entry
	}
	entry.held = file
	return entry
}

func (e diskEntry) file() *File {
	if e.held != nil {
		return e.held
	}
	return e.dropped.Value()
}

type includesEntry struct {
	size     int64
	modTime  time.Time
	includes []model.Include
}

func Detect(_ string, in dialect.Input) dialect.Detection {
	return dialect.Resolve(nil, in)
}

func New(overlay Overlay, resolve func(path string, in dialect.Input) dialect.Detection) *Workspace {
	return &Workspace{overlay: overlay, resolve: resolve, now: time.Now, files: map[string]diskEntry{}, dirs: map[string]dirListing{}, ignores: map[string]ignoreRules{}, missing: map[string]time.Time{}, includes: map[string]includesEntry{}}
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
	clear(w.ignores)
	clear(w.missing)
	w.generation.Add(1)
}

func (w *Workspace) Load(path string) (*File, error) {
	return w.load(path, "")
}

func (w *Workspace) LoadAs(path string, language host.Language) (*File, error) {
	return w.load(path, language.LanguageID())
}

func (w *Workspace) Present(path string) bool {
	if _, open := w.overlay(fileuri.FromPath(path)); open {
		return true
	}
	_, err := regularfile.Stat(path)
	return err == nil
}

func (w *Workspace) NixIncludes(path string) ([]model.Include, int64, error) {
	if f, open := w.overlay(fileuri.FromPath(path)); open && f.Native != nil {
		return f.Native.Index.Includes, int64(len(f.Doc.Source().Text())), nil
	}
	info, err := regularfile.Stat(path)
	if err != nil {
		return nil, 0, fmt.Errorf("reading the imports of %s: %w", path, err)
	}
	key := platform.Native.Fold(path)
	w.mu.Lock()
	cached, ok := w.includes[key]
	w.mu.Unlock()
	if ok && cached.size == info.Size() && cached.modTime.Equal(info.ModTime()) {
		return cached.includes, cached.size, nil
	}
	raw, info, err := regularfile.ReadStat(path, info, maxFileSize)
	if err != nil {
		return nil, 0, fmt.Errorf("reading the imports of %s: %w", path, err)
	}
	includes := slices.DeleteFunc(host.NixIncludes(string(raw)), func(inc model.Include) bool { return inc.Probe || inc.Dynamic })
	for i := range includes {
		includes[i] = includes[i].Detached()
	}
	includes = slices.Clone(includes)
	w.mu.Lock()
	w.includes[key] = includesEntry{size: info.Size(), modTime: info.ModTime(), includes: includes}
	w.mu.Unlock()
	return includes, info.Size(), nil
}

func (f *File) parsedAs(languageID string) bool {
	if languageID == "" {
		return true
	}
	language, ok := host.LanguageOf(languageID)
	return ok && f.Native != nil && f.Native.Language == language
}

func (w *Workspace) load(path, languageID string) (*File, error) {
	now := w.now()
	key := platform.Native.Fold(path)
	w.mu.Lock()
	cached, ok := w.files[key]
	known := cached.file()
	ok = ok && known != nil && known.parsedAs(languageID)
	fresh := ok && (w.watchedLocked(path) || now.Sub(cached.checked) < DiskRecheck)
	w.mu.Unlock()
	var uri string
	if ok {
		uri = known.URI
	} else {
		uri = fileuri.FromPath(path)
	}
	if f, open := w.overlay(uri); open {
		return f, nil
	}
	if fresh {
		return known, nil
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
		return known, nil
	}
	raw, info, err := regularfile.ReadStat(path, info, maxFileSize)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", path, err)
	}
	file := w.parseDisk(path, docstore.NewDocument(uri, languageID, 0, string(raw)))
	w.mu.Lock()
	w.files[key] = newDiskEntry(file, info, now)
	w.mu.Unlock()
	return file, nil
}

func (w *Workspace) parseDisk(path string, doc *docstore.Document) *File {
	if language, ok := host.Of(doc.URI, doc.Language); ok && language == host.Nix && host.NixStoreFile(path) {
		script := host.ProjectDependency(doc)
		return &File{Path: path, URI: doc.URI, Doc: script.Doc, Tree: script.Parse(nil), Detection: script.Detection, Native: script.Native, spans: script.Spans()}
	}
	return Parse(path, doc, nil, func(in dialect.Input) dialect.Detection { return w.resolve(path, in) })
}

func Parse(path string, doc *docstore.Document, previous *parser.Tree, resolve func(dialect.Input) dialect.Detection) *File {
	if language, ok := host.Of(doc.URI, doc.Language); ok {
		script := language.Project(doc)
		file := &File{Path: path, URI: doc.URI, Doc: script.Doc, Tree: script.Parse(previous), Detection: script.Detection, Native: script.Native, spans: script.Spans()}
		for _, v := range script.Variants {
			file.Others = append(file.Others, &File{Path: path, URI: doc.URI, Doc: script.Doc, Tree: script.ParseVariant(v, nil), Detection: v.Detection, Native: script.Native, spans: v.Spans()})
		}
		return file
	}
	detection := resolve(dialect.Input{URI: doc.URI, LanguageID: doc.Language, Text: doc.Text()})
	return &File{Path: path, URI: doc.URI, Doc: doc, Tree: parser.ParseEdit(previous, doc.Text(), detection.Shell), Detection: detection}
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
	modTime    time.Time
	files      []string
	dirs       []string
	ignoreFile bool
}

type ignoreRules struct {
	size    int64
	modTime time.Time
	rules   gitignore.Rules
}

type pendingDir struct {
	path    string
	rel     string
	ignores *gitignore.Stack
}

var ErrScanCut = errors.New("more files than one scan follows")

func (w *Workspace) ShellFiles(ctx context.Context) ([]string, error) {
	return w.scan(ctx, maxScanFiles)
}

func (w *Workspace) ScanCut() bool { return w.scanCut.Load() }

func (w *Workspace) AllShellFiles(ctx context.Context) ([]string, error) {
	return w.scan(ctx, math.MaxInt)
}

func (w *Workspace) scan(ctx context.Context, limit int) ([]string, error) {
	var out []string
	for _, root := range w.Roots() {
		rel, ignores, err := w.enclosingIgnores(root)
		if err != nil {
			return nil, fmt.Errorf("scanning workspace root %s: %w", root, err)
		}
		pending := []pendingDir{{path: root, rel: rel, ignores: ignores}}
		for len(pending) > 0 && len(out) < limit {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			dir := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			listing, err := w.listDir(dir.path)
			if errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("scanning workspace root %s: %w", root, err)
			}
			ignores := dir.ignores
			if listing.ignoreFile {
				rules, err := w.ignoreRules(filepath.Join(dir.path, ignoreFile))
				if err != nil {
					return nil, fmt.Errorf("scanning workspace root %s: %w", root, err)
				}
				ignores = ignores.Push(dir.rel, rules)
			}
			for _, file := range listing.files {
				if !ignores.Ignored(joinRel(dir.rel, filepath.Base(file)), false) {
					out = append(out, file)
				}
			}
			for _, sub := range listing.dirs {
				if rel := joinRel(dir.rel, filepath.Base(sub)); !ignores.Ignored(rel, true) {
					pending = append(pending, pendingDir{path: sub, rel: rel, ignores: ignores})
				}
			}
		}
	}
	slices.Sort(out)
	out = slices.Compact(out)
	if limit != math.MaxInt {
		w.scanCut.Store(len(out) >= limit)
	}
	return out[:min(len(out), limit)], nil
}

func joinRel(rel, name string) string {
	name = platform.Native.Fold(name)
	if rel == "" {
		return name
	}
	return rel + "/" + name
}

func (w *Workspace) NixFiles(dir string) ([]string, error) {
	var out []string
	pending := []string{dir}
	for len(pending) > 0 && len(out) < maxScanFiles {
		at := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		entries, err := os.ReadDir(at)
		if err != nil {
			return nil, fmt.Errorf("listing the Nix files under %s: %w", dir, err)
		}
		for _, entry := range entries {
			path := filepath.Join(at, entry.Name())
			kind := entry.Type()
			if kind&fs.ModeSymlink != 0 {
				info, err := os.Stat(path)
				if err != nil {
					continue
				}
				kind = info.Mode().Type()
			}
			switch {
			case kind.IsDir():
				pending = append(pending, path)
			case kind.IsRegular() && filepath.Ext(path) == nixExtension:
				out = append(out, path)
			}
		}
	}
	slices.Sort(out)
	if len(pending) > 0 {
		return out, fmt.Errorf("listing the Nix files under %s: %w", dir, ErrScanCut)
	}
	return out, nil
}

func (w *Workspace) enclosingIgnores(root string) (string, *gitignore.Stack, error) {
	top, gitDir, found, err := repository(root)
	if err != nil || !found {
		return "", nil, err
	}
	exclude, err := w.ignoreRules(filepath.Join(gitDir, "info", "exclude"))
	if err != nil {
		return "", nil, err
	}
	ignores := (*gitignore.Stack)(nil).Push("", exclude)
	below, _ := platform.RelativeWithin(top, root)
	rel, dir := "", top
	for name := range strings.SplitSeq(filepath.ToSlash(below), "/") {
		if name == "." {
			continue
		}
		rules, err := w.ignoreRules(filepath.Join(dir, ignoreFile))
		if err != nil {
			return "", nil, err
		}
		ignores = ignores.Push(rel, rules)
		rel, dir = joinRel(rel, name), filepath.Join(dir, name)
	}
	return rel, ignores, nil
}

func repository(root string) (top, gitDir string, found bool, err error) {
	for dir := root; ; {
		dotGit := filepath.Join(dir, ".git")
		info, err := os.Lstat(dotGit)
		switch {
		case err == nil && info.IsDir():
			return dir, dotGit, true, nil
		case err == nil:
			gitDir, err := linkedGitDir(dir, dotGit)
			return dir, gitDir, err == nil, err
		case !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fs.ErrPermission):
			return "", "", false, fmt.Errorf("looking for the git repository of %s: %w", root, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", false, nil
		}
		dir = parent
	}
}

func linkedGitDir(top, dotGit string) (string, error) {
	raw, err := regularfile.ReadConfig(dotGit)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", dotGit, err)
	}
	gitDir, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir:")
	if !ok {
		return "", fmt.Errorf("%s does not name a git directory", dotGit)
	}
	gitDir = resolveFrom(top, strings.TrimSpace(gitDir))
	common, err := regularfile.ReadConfig(filepath.Join(gitDir, "commondir"))
	if errors.Is(err, fs.ErrNotExist) {
		return gitDir, nil
	}
	if err != nil {
		return "", fmt.Errorf("reading the common directory of %s: %w", gitDir, err)
	}
	return resolveFrom(gitDir, strings.TrimSpace(string(common))), nil
}

func resolveFrom(dir, path string) string {
	path = filepath.FromSlash(path)
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}

func (w *Workspace) ignoreRules(path string) (gitignore.Rules, error) {
	key := platform.Native.Fold(path)
	info, err := regularfile.Stat(path)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
		w.mu.Lock()
		delete(w.ignores, key)
		w.mu.Unlock()
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading ignore rules %s: %w", path, err)
	}
	w.mu.Lock()
	cached, ok := w.ignores[key]
	w.mu.Unlock()
	if ok && cached.size == info.Size() && cached.modTime.Equal(info.ModTime()) {
		return cached.rules, nil
	}
	raw, info, err := regularfile.ReadStat(path, info, maxFileSize)
	if err != nil {
		return nil, fmt.Errorf("reading ignore rules %s: %w", path, err)
	}
	rules := gitignore.Parse(platform.Native.Fold(string(raw)))
	w.mu.Lock()
	w.ignores[key] = ignoreRules{size: info.Size(), modTime: info.ModTime(), rules: rules}
	w.mu.Unlock()
	return rules, nil
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
		case entry.Name() == ignoreFile:
			listing.ignoreFile = entry.Type().IsRegular()
		case entry.Type().IsRegular() && isShellFile(path, entry):
			listing.files = append(listing.files, path)
		}
	}
	w.mu.Lock()
	if previous, ok := w.dirs[platform.Native.Fold(dir)]; ok {
		w.forgetRemovedLocked(previous, listing)
		if previous.ignoreFile && !listing.ignoreFile {
			delete(w.ignores, platform.Native.Fold(filepath.Join(dir, ignoreFile)))
		}
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
	for key := range w.ignores {
		if platform.Native.Within(key, dir) {
			delete(w.ignores, key)
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
