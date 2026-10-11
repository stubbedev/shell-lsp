package shellcheckrc

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

const minSweep = 256

type ReadFile func(path string) ([]byte, error)

type Resolver struct {
	read     ReadFile
	now      func() time.Time
	recheck  time.Duration
	fallback []string

	mu      sync.Mutex
	files   map[string]cachedFile
	sweepAt int
}

type cachedFile struct {
	file    File
	present bool
	err     error
	checked time.Time
}

func NewResolver(read ReadFile, now func() time.Time, recheck time.Duration, fallback []string) *Resolver {
	return &Resolver{read: read, now: now, recheck: recheck, fallback: fallback, files: map[string]cachedFile{}, sweepAt: minSweep}
}

func Fallback(home, configHome string) []string {
	var out []string
	if configHome == "" && home != "" {
		configHome = filepath.Join(home, ".config")
	}
	if configHome != "" {
		out = append(out, filepath.Join(configHome, "shellcheckrc"))
	}
	if home != "" {
		out = append(out, filepath.Join(home, FileName))
	}
	return out
}

func IsName(path string) bool {
	return slices.Contains(Names, filepath.Base(path))
}

func (r *Resolver) Nearest(dir string) (File, bool, error) {
	for {
		for _, name := range Names {
			if rc, found, err := r.Load(filepath.Join(dir, name)); found || err != nil {
				return rc, found, err
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	for _, path := range r.fallback {
		if rc, found, err := r.Load(path); found || err != nil {
			return rc, found, err
		}
	}
	return File{}, false, nil
}

func (r *Resolver) Load(path string) (File, bool, error) {
	now := r.now()
	r.mu.Lock()
	cached, ok := r.files[path]
	r.mu.Unlock()
	if ok && now.Sub(cached.checked) < r.recheck {
		return cached.file, cached.present, cached.err
	}
	entry := cachedFile{checked: now}
	data, err := r.read(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		entry.err = fmt.Errorf("reading %s: %w", path, err)
	default:
		entry.file, entry.err = Parse(path, string(data))
		entry.present = entry.err == nil
	}
	r.mu.Lock()
	r.files[path] = entry
	if len(r.files) >= r.sweepAt {
		r.sweepLocked(now)
	}
	r.mu.Unlock()
	return entry.file, entry.present, entry.err
}

func (r *Resolver) Invalidate(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.files, path)
}

func (r *Resolver) sweepLocked(now time.Time) {
	for path, cached := range r.files {
		if now.Sub(cached.checked) >= r.recheck {
			delete(r.files, path)
		}
	}
	r.sweepAt = max(minSweep, 2*len(r.files))
}
