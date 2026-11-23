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
	"github.com/stubbedev/shell-lsp/internal/platform"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

const (
	repositoryMarker = ".git"
	ignoreFile       = ".gitignore"
	maxTreeDirs      = 20000
)

type ReadDir func(dir string) ([]fs.DirEntry, error)

type Resolver struct {
	files   *filecache.Cache[file]
	ignores *filecache.Cache[gitignore.Rules]
	readDir ReadDir
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
	repository bool
	ignoreFile bool
	checked    time.Time
	stamp      diskwatch.Stamp
}

type tree struct {
	dirs    []string
	checked time.Time
	stamp   diskwatch.Stamp
}

type pendingDir struct {
	path    string
	rel     string
	ignores *gitignore.Stack
}

func parseIgnore(_ string, data []byte) (gitignore.Rules, error) {
	return gitignore.Parse(platform.Native.Fold(string(data))), nil
}

func NewResolver(read filecache.ReadFile, readDir ReadDir, now func() time.Time, recheck time.Duration) *Resolver {
	return &Resolver{
		files:    filecache.New(read, parse, now, recheck),
		ignores:  filecache.New(read, parseIgnore, now, recheck),
		readDir:  readDir,
		now:      now,
		recheck:  recheck,
		listings: map[string]listing{},
		trees:    map[string]tree{},
	}
}

func (r *Resolver) Watch(disk *diskwatch.Watcher) {
	r.files.Watch(disk)
	r.ignores.Watch(disk)
	r.disk = disk
}

func (r *Resolver) Invalidate(path string) {
	r.files.Invalidate(path)
	r.ignores.Invalidate(path)
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

func (r *Resolver) Dockerfiles(dir string) (Dockerfiles, error) {
	dir = filepath.Clean(dir)
	dirs, err := r.bakeDirs(dir)
	if err != nil {
		return nil, err
	}
	found := Dockerfiles{}
	for _, at := range dirs {
		if err := r.directory(at, found); err != nil {
			return nil, err
		}
	}
	out := Dockerfiles{}
	for path, contexts := range found {
		if filepath.Dir(path) == dir {
			out[path] = contexts
		}
	}
	return out, nil
}

func (r *Resolver) bakeDirs(dir string) ([]string, error) {
	var dirs []string
	root := ""
	for at := dir; ; {
		l, err := r.listing(at)
		if err != nil {
			return nil, err
		}
		if len(l.names) > 0 {
			dirs = append(dirs, at)
		}
		if root == "" && l.repository {
			root = at
		}
		parent := filepath.Dir(at)
		if parent == at {
			break
		}
		at = parent
	}
	if root == "" {
		return dirs, nil
	}
	below, err := r.tree(root)
	if err != nil {
		return nil, err
	}
	dirs = append(dirs, below...)
	slices.Sort(dirs)
	return slices.Compact(dirs), nil
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

func (r *Resolver) tree(root string) ([]string, error) {
	now := r.now()
	r.mu.Lock()
	cached, ok := r.trees[root]
	r.mu.Unlock()
	if ok && (now.Sub(cached.checked) < r.recheck || r.disk.Unchanged(cached.stamp)) {
		return cached.dirs, nil
	}
	start := r.disk.Current()
	fresh := tree{checked: now}
	pending := []pendingDir{{path: root}}
	for visited := 0; len(pending) > 0 && visited < maxTreeDirs; visited++ {
		at := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		l, err := r.listing(at.path)
		if err != nil {
			return nil, fmt.Errorf("looking for bake files under %s: %w", root, err)
		}
		if len(l.names) > 0 {
			fresh.dirs = append(fresh.dirs, at.path)
		}
		ignores := at.ignores
		if l.ignoreFile {
			rules, _, err := r.ignores.Load(filepath.Join(at.path, ignoreFile))
			if err != nil {
				return nil, fmt.Errorf("looking for bake files under %s: %w", root, err)
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
	return fresh.dirs, nil
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
		case name == repositoryMarker:
			fresh.repository = true
		case e.IsDir():
			if !workspace.SkipsDir(name) {
				fresh.dirs = append(fresh.dirs, name)
			}
		case name == ignoreFile:
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
