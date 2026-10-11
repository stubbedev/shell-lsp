package editorconfig

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const FileName = ".editorconfig"

const minSweep = 256

const (
	unset         = "unset"
	byteOrderMark = "\uFEFF"
)

type Properties map[string]string

type File struct {
	Root     bool
	sections []section
}

type section struct {
	glob     pattern
	language string
	values   [][2]string
}

func Parse(data []byte, dir string) (File, error) {
	var f File
	var current *section
	for number, raw := range strings.Split(strings.TrimPrefix(string(data), byteOrderMark), "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == "", line[0] == '#', line[0] == ';':
			continue
		case line[0] == '[' && line[len(line)-1] == ']':
			s, err := newSection(line[1:len(line)-1], dir)
			if err != nil {
				return File{}, fmt.Errorf("line %d: %w", number+1, err)
			}
			f.sections = append(f.sections, s)
			current = &f.sections[len(f.sections)-1]
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return File{}, fmt.Errorf("line %d: %q is neither a section, a property nor a comment", number+1, line)
		}
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		if current == nil {
			if key == "root" {
				f.Root = strings.EqualFold(value, "true")
			}
			continue
		}
		current.values = append(current.values, [2]string{key, value})
	}
	return f, nil
}

func newSection(header, dir string) (section, error) {
	if language, ok := strings.CutPrefix(header, "["); ok && strings.HasSuffix(language, "]") {
		return section{language: strings.ToLower(strings.TrimSuffix(language, "]"))}, nil
	}
	glob, err := compilePattern(header, filepath.ToSlash(dir))
	if err != nil {
		return section{}, err
	}
	return section{glob: glob}, nil
}

func (f File) apply(props Properties, path string, languages []string) {
	slashed := filepath.ToSlash(path)
	for _, s := range f.sections {
		if s.language != "" && !slices.Contains(languages, s.language) {
			continue
		}
		if s.language == "" && !s.glob.matches(slashed) {
			continue
		}
		for _, kv := range s.values {
			if strings.EqualFold(kv[1], unset) {
				delete(props, kv[0])
				continue
			}
			props[kv[0]] = kv[1]
		}
	}
}

type ReadFile func(path string) ([]byte, error)

type Resolver struct {
	read    ReadFile
	now     func() time.Time
	recheck time.Duration

	mu      sync.Mutex
	files   map[string]cachedFile
	sweepAt int
}

type cachedFile struct {
	file    File
	present bool
	checked time.Time
}

func NewResolver(read ReadFile, now func() time.Time, recheck time.Duration) *Resolver {
	return &Resolver{read: read, now: now, recheck: recheck, files: map[string]cachedFile{}, sweepAt: minSweep}
}

func (r *Resolver) Properties(path string, languages []string) (Properties, error) {
	var chain []File
	for dir := filepath.Dir(path); ; {
		f, present, err := r.load(filepath.Join(dir, FileName), dir)
		if err != nil {
			return nil, err
		}
		if present {
			chain = append(chain, f)
			if f.Root {
				break
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	props := Properties{}
	for _, f := range slices.Backward(chain) {
		f.apply(props, path, languages)
	}
	return props, nil
}

func (r *Resolver) Invalidate(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.files, path)
}

func (r *Resolver) load(path, dir string) (File, bool, error) {
	now := r.now()
	r.mu.Lock()
	cached, ok := r.files[path]
	r.mu.Unlock()
	if ok && now.Sub(cached.checked) < r.recheck {
		return cached.file, cached.present, nil
	}
	data, err := r.read(path)
	entry := cachedFile{checked: now}
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return File{}, false, fmt.Errorf("reading %s: %w", path, err)
	default:
		f, err := Parse(data, dir)
		if err != nil {
			return File{}, false, fmt.Errorf("%s: %w", path, err)
		}
		entry.file, entry.present = f, true
	}
	r.mu.Lock()
	r.files[path] = entry
	if len(r.files) >= r.sweepAt {
		r.sweepLocked(now)
	}
	r.mu.Unlock()
	return entry.file, entry.present, nil
}

func (r *Resolver) sweepLocked(now time.Time) {
	for path, cached := range r.files {
		if now.Sub(cached.checked) >= r.recheck {
			delete(r.files, path)
		}
	}
	r.sweepAt = max(minSweep, 2*len(r.files))
}
