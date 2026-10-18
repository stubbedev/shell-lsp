package filecache

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sync"
	"time"
)

const minSweep = 256

type ReadFile func(path string) ([]byte, error)

type Parse[T any] func(path string, data []byte) (T, error)

type Cache[T any] struct {
	read    ReadFile
	parse   Parse[T]
	now     func() time.Time
	recheck time.Duration

	mu      sync.Mutex
	entries map[string]entry[T]
	sweepAt int
}

type entry[T any] struct {
	value   T
	present bool
	err     error
	checked time.Time
}

func New[T any](read ReadFile, parse Parse[T], now func() time.Time, recheck time.Duration) *Cache[T] {
	return &Cache[T]{read: read, parse: parse, now: now, recheck: recheck, entries: map[string]entry[T]{}, sweepAt: minSweep}
}

func (c *Cache[T]) Load(path string) (T, bool, error) {
	now := c.now()
	c.mu.Lock()
	cached, ok := c.entries[path]
	c.mu.Unlock()
	if ok && now.Sub(cached.checked) < c.recheck {
		return cached.value, cached.present, cached.err
	}
	fresh := entry[T]{checked: now}
	data, err := c.read(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		fresh.err = fmt.Errorf("reading %s: %w", path, err)
	default:
		fresh.value, fresh.err = c.parse(path, data)
		fresh.present = fresh.err == nil
	}
	c.mu.Lock()
	c.entries[path] = fresh
	if len(c.entries) >= c.sweepAt {
		c.sweepLocked(now)
	}
	c.mu.Unlock()
	return fresh.value, fresh.present, fresh.err
}

func (c *Cache[T]) Invalidate(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, path)
}

func (c *Cache[T]) sweepLocked(now time.Time) {
	for path, cached := range c.entries {
		if now.Sub(cached.checked) >= c.recheck {
			delete(c.entries, path)
		}
	}
	c.sweepAt = max(minSweep, 2*len(c.entries))
}

func (c *Cache[T]) Nearest(dir string, names, fallback []string) (T, bool, error) {
	for {
		for _, name := range names {
			if value, found, err := c.Load(filepath.Join(dir, name)); found || err != nil {
				return value, found, err
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	for _, path := range fallback {
		if value, found, err := c.Load(path); found || err != nil {
			return value, found, err
		}
	}
	var zero T
	return zero, false, nil
}
