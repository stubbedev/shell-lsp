package diskwatch

import (
	"path/filepath"
	"sync"
	"sync/atomic"
)

type Stamp struct {
	generation uint64
	tracked    bool
}

type watchedDir struct {
	names    map[string]struct{}
	children map[string]struct{}
	listed   bool
}

type Watcher struct {
	source     eventSource
	immutable  func(path string) bool
	generation atomic.Uint64
	partial    atomic.Bool

	mu      sync.Mutex
	byPath  map[string]*watchedDir
	byWd    map[int32]*watchedDir
	refused map[string]struct{}
	buffer  []byte
	closed  bool
}

type kernelEvent struct {
	wd   int32
	mask eventMask
	name []byte
}

type eventMask uint8

const (
	eventLinked eventMask = 1 << iota
	eventLost
	eventOverflow
)

type eventSource interface {
	add(dir string) (int32, error)
	remove(wd int32)
	read(buffer []byte, handle func(kernelEvent)) error
	close()
	inspect(path string) (pathKind, error)
	remote(dir string, device uint64) bool
}

type pathKind struct {
	symlink bool
	device  uint64
}

const eventBufferSize = 64 << 10

func newWatcher(source eventSource) *Watcher {
	return &Watcher{
		source:  source,
		byPath:  map[string]*watchedDir{},
		byWd:    map[int32]*watchedDir{},
		refused: map[string]struct{}{},
		buffer:  make([]byte, eventBufferSize),
	}
}

func (w *Watcher) TrustImmutable(immutable func(path string) bool) {
	if w != nil {
		w.immutable = immutable
	}
}

func (w *Watcher) Close() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.closed = true
	w.source.close()
}

func (w *Watcher) Unchanged(s Stamp) bool {
	return w != nil && s.tracked && s.generation == w.generation.Load()
}

func (w *Watcher) Current() Stamp {
	if w == nil || w.partial.Load() {
		return Stamp{}
	}
	return Stamp{generation: w.generation.Load(), tracked: true}
}

func (s Stamp) And(other Stamp) Stamp {
	if !s.tracked || !other.tracked {
		return Stamp{}
	}
	return Stamp{generation: min(s.generation, other.generation), tracked: true}
}

func (w *Watcher) Drain() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || len(w.byWd) == 0 {
		return
	}
	if err := w.source.read(w.buffer, w.handle); err != nil {
		w.resetLocked()
	}
}

func (w *Watcher) handle(e kernelEvent) {
	if e.mask&eventOverflow != 0 {
		w.resetLocked()
		return
	}
	d := w.byWd[e.wd]
	if d == nil {
		return
	}
	if e.mask&eventLost != 0 {
		w.resetLocked()
		return
	}
	if len(e.name) == 0 {
		return
	}
	if e.mask&eventLinked != 0 {
		if _, watchedChild := d.children[string(e.name)]; watchedChild {
			w.resetLocked()
			return
		}
	}
	if _, tracked := d.names[string(e.name)]; tracked {
		delete(d.names, string(e.name))
		w.generation.Add(1)
		return
	}
	if d.listed && e.mask&eventLinked != 0 {
		w.generation.Add(1)
	}
}

func (w *Watcher) resetLocked() {
	for wd := range w.byWd {
		w.source.remove(wd)
	}
	clear(w.byWd)
	clear(w.byPath)
	w.generation.Add(1)
}

func (w *Watcher) Track(path string) Stamp {
	if w == nil {
		return Stamp{}
	}
	stamp := Stamp{generation: w.generation.Load(), tracked: true}
	path = filepath.Clean(path)
	if w.immutable != nil && w.immutable(path) {
		return stamp
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || !w.trackLocked(path) {
		w.partial.Store(true)
		return Stamp{}
	}
	return stamp
}

func (w *Watcher) TrackDir(dir string) Stamp {
	if w == nil {
		return Stamp{}
	}
	stamp := Stamp{generation: w.generation.Load(), tracked: true}
	dir = filepath.Clean(dir)
	if w.immutable != nil && w.immutable(dir) {
		return stamp
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		w.partial.Store(true)
		return Stamp{}
	}
	d, ok := w.ensureLocked(dir)
	if !ok {
		w.partial.Store(true)
		return Stamp{}
	}
	if d != nil {
		d.listed = true
	}
	return stamp
}

func (w *Watcher) trackLocked(path string) bool {
	dir, name := filepath.Split(path)
	dir = filepath.Clean(dir)
	d, ok := w.ensureLocked(dir)
	if !ok {
		return false
	}
	if d == nil {
		return true
	}
	if _, known := d.names[name]; known {
		return true
	}
	kind, err := w.source.inspect(path)
	d.names[name] = struct{}{}
	if err != nil || !kind.symlink {
		return true
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	if target == path || w.immutable != nil && w.immutable(target) {
		return true
	}
	return w.trackLocked(target)
}

func (w *Watcher) ensureLocked(dir string) (*watchedDir, bool) {
	if d := w.byPath[dir]; d != nil {
		return d, true
	}
	if _, refused := w.refused[dir]; refused {
		return nil, false
	}
	parent := filepath.Dir(dir)
	if parent != dir {
		up, ok := w.ensureLocked(parent)
		if !ok {
			return nil, false
		}
		if up != nil {
			up.children[filepath.Base(dir)] = struct{}{}
		}
	}
	kind, err := w.source.inspect(dir)
	if isAbsent(err) {
		return nil, true
	}
	if err != nil || w.source.remote(dir, kind.device) {
		w.refused[dir] = struct{}{}
		return nil, false
	}
	if kind.symlink {
		resolved, err := filepath.EvalSymlinks(dir)
		if isAbsent(err) {
			return nil, true
		}
		if err != nil {
			return nil, false
		}
		if _, ok := w.ensureLocked(resolved); !ok {
			return nil, false
		}
	}
	wd, err := w.source.add(dir)
	if isAbsent(err) {
		return nil, true
	}
	if err != nil {
		w.refused[dir] = struct{}{}
		return nil, false
	}
	d := w.byWd[wd]
	if d == nil {
		d = &watchedDir{names: map[string]struct{}{}, children: map[string]struct{}{}}
		w.byWd[wd] = d
	}
	w.byPath[dir] = d
	return d, true
}
