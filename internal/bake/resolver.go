package bake

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sync"
	"time"

	"github.com/stubbedev/shell-lsp/internal/diskwatch"
	"github.com/stubbedev/shell-lsp/internal/filecache"
)

type ReadDir func(dir string) ([]fs.DirEntry, error)

type Resolver struct {
	files   *filecache.Cache[[]target]
	list    ReadDir
	now     func() time.Time
	recheck time.Duration
	disk    *diskwatch.Watcher

	mu       sync.Mutex
	listings map[string]listing
}

type listing struct {
	names   []string
	checked time.Time
	stamp   diskwatch.Stamp
}

func NewResolver(read filecache.ReadFile, list ReadDir, now func() time.Time, recheck time.Duration) *Resolver {
	return &Resolver{files: filecache.New(read, parse, now, recheck), list: list, now: now, recheck: recheck, listings: map[string]listing{}}
}

func (r *Resolver) Watch(disk *diskwatch.Watcher) {
	r.files.Watch(disk)
	r.disk = disk
}

func (r *Resolver) Invalidate(path string) {
	r.files.Invalidate(path)
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.listings, filepath.Dir(path))
}

func (r *Resolver) Dockerfiles(dir string) (Dockerfiles, error) {
	dir = filepath.Clean(dir)
	found := Dockerfiles{}
	for at := dir; ; {
		names, err := r.names(at)
		if err != nil {
			return nil, err
		}
		for _, project := range projects(names) {
			var targets []target
			for _, name := range project {
				parsed, _, err := r.files.Load(filepath.Join(at, name))
				if err != nil {
					return nil, err
				}
				targets = append(targets, parsed...)
			}
			builds(at, targets, found)
		}
		parent := filepath.Dir(at)
		if parent == at {
			break
		}
		at = parent
	}
	out := Dockerfiles{}
	for path, contexts := range found {
		if filepath.Dir(path) == dir {
			out[path] = contexts
		}
	}
	return out, nil
}

func (r *Resolver) names(dir string) ([]string, error) {
	now := r.now()
	r.mu.Lock()
	cached, ok := r.listings[dir]
	r.mu.Unlock()
	if ok && (now.Sub(cached.checked) < r.recheck || r.disk.Unchanged(cached.stamp)) {
		return cached.names, nil
	}
	fresh := listing{checked: now, stamp: r.disk.TrackDir(dir)}
	entries, err := r.list(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, fs.ErrPermission):
		entries = nil
	case err != nil:
		return nil, fmt.Errorf("listing %s for bake files: %w", dir, err)
	}
	for _, e := range entries {
		if !e.IsDir() && IsName(e.Name()) {
			fresh.names = append(fresh.names, e.Name())
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listings[dir] = fresh
	return fresh.names, nil
}
