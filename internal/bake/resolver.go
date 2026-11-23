package bake

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/stubbedev/shell-lsp/internal/diskwatch"
	"github.com/stubbedev/shell-lsp/internal/filecache"
	"github.com/stubbedev/shell-lsp/internal/gitignore"
	"github.com/stubbedev/shell-lsp/internal/gitrepo"
	"github.com/stubbedev/shell-lsp/internal/platform"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

const maxTreeDirs = 20000

type Cut struct {
	Root  string
	Limit int
}

func (c Cut) String() string {
	return fmt.Sprintf("looking for bake files under %s stopped after %d directories, so contexts that bake files past them define are not known", c.Root, c.Limit)
}

type Found struct {
	Dockerfiles Dockerfiles
	Cut         *Cut
}

type ReadDir func(dir string) ([]fs.DirEntry, error)

type Resolver struct {
	files   *filecache.Cache[file]
	git     *gitrepo.Ignores
	readDir ReadDir
	limit   int
	now     func() time.Time
	recheck time.Duration
	disk    *diskwatch.Watcher

	mu       sync.Mutex
	listings map[string]listing
	trees    map[string]tree
}

type listing struct {
	names      []string
	dirs       []string
	ignoreFile bool
	checked    time.Time
	stamp      diskwatch.Stamp
}

type tree struct {
	dirs    []string
	cut     bool
	checked time.Time
	stamp   diskwatch.Stamp
}

type pendingDir struct {
	path    string
	rel     string
	ignores *gitignore.Stack
}

func NewResolver(read filecache.ReadFile, readDir ReadDir, now func() time.Time, recheck time.Duration, git gitrepo.Environment) *Resolver {
	return &Resolver{
		files:    filecache.New(read, parse, now, recheck),
		git:      gitrepo.NewIgnores(git, read, now, recheck),
		readDir:  readDir,
		limit:    maxTreeDirs,
		now:      now,
		recheck:  recheck,
		listings: map[string]listing{},
		trees:    map[string]tree{},
	}
}

func (r *Resolver) Watch(disk *diskwatch.Watcher) {
	r.files.Watch(disk)
	r.git.Watch(disk)
	r.disk = disk
}

func (r *Resolver) Invalidate(path string) {
	r.files.Invalidate(path)
	r.git.Invalidate(path)
	r.mu.Lock()
	defer r.mu.Unlock()
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		delete(r.listings, dir)
		if filepath.Dir(dir) == dir {
			break
		}
	}
	maps.DeleteFunc(r.trees, func(root string, _ tree) bool { return platform.Native.Within(path, root) })
}

func (r *Resolver) InvalidateAll() {
	r.files.InvalidateAll()
	r.git.InvalidateAll()
	r.mu.Lock()
	defer r.mu.Unlock()
	clear(r.listings)
	clear(r.trees)
}

func (r *Resolver) Dockerfiles(dir string) (Found, error) {
	dir = filepath.Clean(dir)
	search, err := r.bakeDirs(dir)
	if err != nil {
		return Found{}, err
	}
	all := Dockerfiles{}
	for _, at := range search.dirs {
		if err := r.directory(at, all); err != nil {
			return Found{}, err
		}
	}
	out := Found{Dockerfiles: Dockerfiles{}, Cut: search.cut}
	for path, contexts := range all {
		if slices.Contains(search.names, filepath.Dir(path)) {
			out.Dockerfiles.merge(filepath.Join(dir, filepath.Base(path)), contexts)
		}
	}
	return out, nil
}

type bakeSearch struct {
	dirs  []string
	names []string
	cut   *Cut
}

func (r *Resolver) bakeDirs(dir string) (bakeSearch, error) {
	var dirs []string
	for at := dir; ; {
		l, err := r.listing(at)
		if err != nil {
			return bakeSearch{}, err
		}
		if len(l.names) > 0 {
			dirs = append(dirs, at)
		}
		parent := filepath.Dir(at)
		if parent == at {
			break
		}
		at = parent
	}
	repo, found, err := r.git.Repository(dir)
	if err != nil || !found {
		return bakeSearch{dirs: dirs, names: []string{dir}}, err
	}
	below, err := r.tree(repo)
	if err != nil {
		return bakeSearch{}, fmt.Errorf("looking for bake files under %s: %w", repo.Top, err)
	}
	dirs = append(dirs, below.dirs...)
	slices.Sort(dirs)
	var cut *Cut
	if below.cut {
		cut = &Cut{Root: repo.Top, Limit: r.limit}
	}
	return bakeSearch{dirs: slices.Compact(dirs), names: []string{dir, filepath.Join(repo.Top, repo.Prefix)}, cut: cut}, nil
}

func (r *Resolver) directory(dir string, into Dockerfiles) error {
	l, err := r.listing(dir)
	if err != nil {
		return err
	}
	var loaded [][]file
	for _, project := range projects(l.names) {
		files := make([]file, 0, len(project))
		for _, name := range project {
			parsed, _, err := r.files.Load(filepath.Join(dir, name))
			if err != nil {
				return err
			}
			files = append(files, parsed)
		}
		loaded = append(loaded, files)
	}
	builds(dir, loaded, into)
	return nil
}

func (r *Resolver) tree(repo gitrepo.Repository) (tree, error) {
	root := repo.Top
	now := r.now()
	r.mu.Lock()
	cached, ok := r.trees[root]
	r.mu.Unlock()
	if ok && (now.Sub(cached.checked) < r.recheck || r.disk.Unchanged(cached.stamp)) {
		return cached, nil
	}
	start := r.disk.Current()
	fresh := tree{checked: now}
	excluded, err := r.git.Base(repo)
	if err != nil {
		return tree{}, err
	}
	pending := []pendingDir{{path: root, ignores: excluded}}
	for visited := 0; len(pending) > 0; visited++ {
		if visited == r.limit {
			fresh.cut = true
			break
		}
		at := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		l, err := r.listing(at.path)
		if err != nil {
			return tree{}, err
		}
		if len(l.names) > 0 {
			fresh.dirs = append(fresh.dirs, at.path)
		}
		ignores := at.ignores
		if l.ignoreFile {
			rules, err := r.git.Rules(filepath.Join(at.path, gitrepo.IgnoreFile))
			if err != nil {
				return tree{}, err
			}
			ignores = ignores.Push(at.rel, rules)
		}
		for _, sub := range l.dirs {
			if rel := workspace.JoinRel(at.rel, sub); !ignores.Ignored(rel, true) {
				pending = append(pending, pendingDir{path: filepath.Join(at.path, sub), rel: rel, ignores: ignores})
			}
		}
	}
	slices.Sort(fresh.dirs)
	fresh.stamp = start.And(r.disk.Current())
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trees[root] = fresh
	return fresh, nil
}

func (r *Resolver) listing(dir string) (listing, error) {
	now := r.now()
	r.mu.Lock()
	cached, ok := r.listings[dir]
	r.mu.Unlock()
	if ok && (now.Sub(cached.checked) < r.recheck || r.disk.Unchanged(cached.stamp)) {
		return cached, nil
	}
	fresh := listing{checked: now, stamp: r.disk.TrackDir(dir)}
	entries, err := r.readDir(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, fs.ErrPermission):
		entries = nil
	case err != nil:
		return listing{}, fmt.Errorf("listing %s for bake files: %w", dir, err)
	}
	for _, e := range entries {
		name := e.Name()
		switch {
		case e.IsDir():
			if !workspace.SkipsDir(name) {
				fresh.dirs = append(fresh.dirs, name)
			}
		case name == gitrepo.IgnoreFile:
			fresh.ignoreFile = true
		case IsName(name):
			fresh.names = append(fresh.names, name)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listings[dir] = fresh
	return fresh, nil
}
