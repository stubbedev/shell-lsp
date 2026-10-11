package manpage

import (
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

const maxRedirects = 4

var (
	commandSections  = []string{"1", "8", "6"}
	defaultManDirs   = []string{"/usr/share/man", "/usr/local/share/man", "/usr/local/man", "/opt/homebrew/share/man"}
	readableSuffixes = []string{"", ".gz", ".bz2"}
)

type library struct {
	dirs       []string
	once       sync.Once
	pages      map[string]string
	names      []string
	unreadable []error
}

func newLibrary(manpath, path string) *library {
	return &library{dirs: manDirs(manpath, path)}
}

func manDirs(manpath, path string) []string {
	var dirs []string
	add := func(d string) {
		if d = filepath.Clean(d); !slices.Contains(dirs, d) {
			dirs = append(dirs, d)
		}
	}
	defaults := func() {
		for _, bin := range filepath.SplitList(path) {
			if bin == "" {
				continue
			}
			prefix := filepath.Dir(filepath.Clean(bin))
			add(filepath.Join(prefix, "share", "man"))
			add(filepath.Join(prefix, "man"))
		}
		for _, d := range defaultManDirs {
			add(d)
		}
	}
	if manpath == "" {
		defaults()
		return dirs
	}
	for _, d := range filepath.SplitList(manpath) {
		if d == "" {
			defaults()
			continue
		}
		add(d)
	}
	return dirs
}

func (l *library) index() map[string]string {
	l.once.Do(l.scan)
	return l.pages
}

func (l *library) prefixed(prefix string) []string {
	l.once.Do(l.scan)
	start, _ := slices.BinarySearch(l.names, prefix)
	end := start
	for end < len(l.names) && strings.HasPrefix(l.names[end], prefix) {
		end++
	}
	return l.names[start:end]
}

func (l *library) scan() {
	l.pages = map[string]string{}
	for _, sec := range commandSections {
		for _, dir := range l.dirs {
			entries, err := os.ReadDir(filepath.Join(dir, "man"+sec))
			if err != nil {
				if !errors.Is(err, fs.ErrNotExist) {
					l.unreadable = append(l.unreadable, err)
				}
				continue
			}
			for _, e := range entries {
				name, suffix, ok := strings.Cut(e.Name(), "."+sec)
				if !ok || !slices.Contains(readableSuffixes, suffix) {
					continue
				}
				if _, seen := l.pages[name]; !seen {
					l.pages[name] = filepath.Join(dir, "man"+sec, e.Name())
				}
			}
		}
	}
	l.names = slices.Sorted(maps.Keys(l.pages))
}

func (l *library) source(name string) ([]byte, error) {
	path, ok := l.index()[name]
	if !ok {
		missing := fmt.Errorf("%w for %s in %s", ErrNoPage, name, strings.Join(l.dirs, ":"))
		return nil, errors.Join(append([]error{missing}, l.unreadable...)...)
	}
	src, err := readPage(path)
	for range maxRedirects {
		if err != nil {
			return nil, err
		}
		target, redirected := redirect(src)
		if !redirected {
			return src, nil
		}
		src, path, err = readRedirect([]string{filepath.Dir(filepath.Dir(path)), filepath.Dir(path)}, target)
	}
	return nil, fmt.Errorf("%w for %s: more than %d .so redirections", ErrNoPage, name, maxRedirects)
}

func readPage(path string) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("opening manual page %s: %w", path, err)
	}
	defer f.Close()
	var r io.Reader = f
	switch filepath.Ext(path) {
	case ".gz":
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, fmt.Errorf("decompressing manual page %s: %w", path, err)
		}
		src, err := io.ReadAll(gz)
		if err == nil {
			err = gz.Close()
		}
		if err != nil {
			return nil, fmt.Errorf("decompressing manual page %s: %w", path, err)
		}
		return src, nil
	case ".bz2":
		r = bzip2.NewReader(f)
	}
	src, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading manual page %s: %w", path, err)
	}
	return src, nil
}

func redirect(src []byte) (string, bool) {
	for line := range bytes.Lines(src) {
		trimmed := strings.TrimSpace(string(line))
		if trimmed == "" || strings.HasPrefix(trimmed, `.\"`) || strings.HasPrefix(trimmed, `'\"`) {
			continue
		}
		target, ok := strings.CutPrefix(trimmed, ".so ")
		return strings.TrimSpace(target), ok
	}
	return "", false
}

func readRedirect(roots []string, target string) ([]byte, string, error) {
	if !filepath.IsLocal(target) {
		return nil, "", fmt.Errorf("%w: .so target %s leaves the manual page tree", ErrNoPage, target)
	}
	for _, root := range roots {
		for _, suffix := range readableSuffixes {
			candidate := filepath.Join(root, target+suffix)
			src, err := readPage(candidate)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return src, candidate, err
		}
	}
	return nil, "", fmt.Errorf("%w: .so target %s not found under %s", ErrNoPage, target, strings.Join(roots, " or "))
}
