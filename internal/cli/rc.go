package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/directive"
)

type rcFile struct {
	path        string
	disabled    codeSet
	enabled     []string
	shell       *dialect.Dialect
	sourcePaths []string
	extended    *bool
}

var rcNames = []string{".shellcheckrc", "shellcheckrc"}

func parseRC(path, text string) (rcFile, error) {
	rc := rcFile{path: path}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if err := rc.apply(line); err != nil {
			return rcFile{}, fmt.Errorf("%s:%d: %w", path, i+1, err)
		}
	}
	return rc, nil
}

func (rc *rcFile) apply(line string) error {
	d, ok := directive.ParseSettings(line)
	if !ok || d.Problem != nil {
		return fmt.Errorf("expected key=value settings such as disable=SC2034, got %q", line)
	}
	for _, pair := range d.Pairs {
		if err := rc.set(pair); err != nil {
			return fmt.Errorf("%s=%s: %w", pair.Key, pair.Value, err)
		}
	}
	return nil
}

func (rc *rcFile) set(pair directive.Pair) error {
	switch pair.Key {
	case directive.Disable:
		set, err := parseCodeSet(pair.Value)
		if err != nil {
			return err
		}
		rc.disabled = rc.disabled.union(set)
	case directive.Enable:
		rc.enabled = append(rc.enabled, splitList(pair.Value)...)
	case directive.Shell:
		d, ok := dialect.FromName(pair.Value)
		if !ok {
			return fmt.Errorf("unknown shell %q", pair.Value)
		}
		rc.shell = &d
	case directive.SourcePath:
		for _, dir := range filepath.SplitList(pair.Value) {
			if dir != scriptDir && !filepath.IsAbs(dir) {
				dir = filepath.Join(filepath.Dir(rc.path), dir)
			}
			rc.sourcePaths = append(rc.sourcePaths, dir)
		}
	case directive.ExternalSources:
		if _, err := strconv.ParseBool(pair.Value); err != nil {
			return fmt.Errorf("want true or false: %w", err)
		}
	case directive.ExtendedAnalysis:
		b, err := strconv.ParseBool(pair.Value)
		if err != nil {
			return fmt.Errorf("want true or false: %w", err)
		}
		rc.extended = &b
	default:
		return fmt.Errorf("%q is not a .shellcheckrc key; known keys are disable, enable, shell, source-path, external-sources and extended-analysis", pair.Key)
	}
	return nil
}

const scriptDir = "SCRIPTDIR"

type rcFinder struct {
	choice   rcChoice
	read     func(path string) ([]byte, error)
	fallback []string

	mu    sync.Mutex
	byDir map[string]rcLookup
	files map[string]rcLookup
}

type rcLookup struct {
	rc  rcFile
	err error
}

func newRCFinder(choice rcChoice, read func(string) ([]byte, error), home, configHome string) *rcFinder {
	var fallback []string
	if configHome == "" && home != "" {
		configHome = filepath.Join(home, ".config")
	}
	if configHome != "" {
		fallback = append(fallback, filepath.Join(configHome, "shellcheckrc"))
	}
	if home != "" {
		fallback = append(fallback, filepath.Join(home, ".shellcheckrc"))
	}
	return &rcFinder{choice: choice, read: read, fallback: fallback, byDir: map[string]rcLookup{}, files: map[string]rcLookup{}}
}

func (f *rcFinder) forDir(dir string) (rcFile, error) {
	switch f.choice.mode {
	case rcNone:
		return rcFile{}, nil
	case rcExplicit:
		rc, found, err := f.load(f.choice.path)
		if err == nil && !found {
			err = fmt.Errorf("reading --rcfile %s: %w", f.choice.path, fs.ErrNotExist)
		}
		return rc, err
	}
	f.mu.Lock()
	cached, ok := f.byDir[dir]
	f.mu.Unlock()
	if ok {
		return cached.rc, cached.err
	}
	rc, err := f.search(dir)
	f.mu.Lock()
	f.byDir[dir] = rcLookup{rc: rc, err: err}
	f.mu.Unlock()
	return rc, err
}

func (f *rcFinder) search(dir string) (rcFile, error) {
	for {
		for _, name := range rcNames {
			if rc, found, err := f.load(filepath.Join(dir, name)); found || err != nil {
				return rc, err
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	for _, path := range f.fallback {
		if rc, found, err := f.load(path); found || err != nil {
			return rc, err
		}
	}
	return rcFile{}, nil
}

func (f *rcFinder) load(path string) (rcFile, bool, error) {
	f.mu.Lock()
	cached, ok := f.files[path]
	f.mu.Unlock()
	if ok {
		return cached.rc, cached.rc.path != "" || cached.err != nil, cached.err
	}
	var lookup rcLookup
	data, err := f.read(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		lookup.err = fmt.Errorf("reading %s: %w", path, err)
	default:
		lookup.rc, lookup.err = parseRC(path, string(data))
	}
	f.mu.Lock()
	f.files[path] = lookup
	f.mu.Unlock()
	return lookup.rc, lookup.rc.path != "" || lookup.err != nil, lookup.err
}
