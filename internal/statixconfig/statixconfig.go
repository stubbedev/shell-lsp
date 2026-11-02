package statixconfig

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/stubbedev/shell-lsp/internal/diskwatch"
	"github.com/stubbedev/shell-lsp/internal/filecache"
	"github.com/stubbedev/shell-lsp/internal/gitignore"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

const FileName = "statix.toml"

const (
	disabledKey = "disabled"
	ignoreKey   = "ignore"
)

type Config struct {
	Dir      string
	Disabled []lint.Code
	Ignored  *gitignore.Stack
}

func Parse(path, text string) (Config, error) {
	cfg := Config{Dir: filepath.Dir(path)}
	values, err := parseTOML(text)
	if err != nil {
		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}
	disabled, err := stringArray(values, disabledKey)
	if err != nil {
		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}
	for _, name := range disabled {
		for _, l := range lint.StatixLints() {
			if l.Name == name {
				cfg.Disabled = append(cfg.Disabled, l.Code)
			}
		}
	}
	ignored, err := stringArray(values, ignoreKey)
	if err != nil {
		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}
	cfg.Ignored = (*gitignore.Stack)(nil).Push("", gitignore.Parse(strings.Join(ignored, "\n")))
	return cfg, nil
}

func stringArray(values map[string]value, key string) ([]string, error) {
	v, set := values[key]
	if !set {
		return nil, nil
	}
	if v.kind != arrayValue {
		return nil, fmt.Errorf("line %d: `%s` is %s, but statix expects an array of strings", v.line, key, v.kind)
	}
	out := make([]string, len(v.items))
	for i, item := range v.items {
		if item.kind != stringValue {
			return nil, fmt.Errorf("line %d: an item of `%s` is %s, but statix expects a string", item.line, key, item.kind)
		}
		out[i] = item.text
	}
	return out, nil
}

func (c Config) ExcludedFor(path string) []lint.Code {
	if c.ignores(path) {
		return allStatixCodes()
	}
	return c.Disabled
}

func (c Config) ignores(path string) bool {
	rel, err := filepath.Rel(c.Dir, path)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	if c.Ignored.Ignored(rel, false) {
		return true
	}
	for at := 0; ; {
		next := strings.IndexByte(rel[at:], '/')
		if next < 0 {
			return false
		}
		at += next
		if c.Ignored.Ignored(rel[:at], true) {
			return true
		}
		at++
	}
}

func allStatixCodes() []lint.Code {
	lints := lint.StatixLints()
	out := make([]lint.Code, len(lints))
	for i, l := range lints {
		out[i] = l.Code
	}
	return out
}

type Resolver struct {
	files *filecache.Cache[Config]
}

func NewResolver(read filecache.ReadFile, now func() time.Time, recheck time.Duration) *Resolver {
	parse := func(path string, data []byte) (Config, error) { return Parse(path, string(data)) }
	return &Resolver{files: filecache.New(read, parse, now, recheck)}
}

func (r *Resolver) Watch(disk *diskwatch.Watcher) { r.files.Watch(disk) }

func (r *Resolver) Invalidate(path string) { r.files.Invalidate(path) }

func (r *Resolver) ExcludedFor(path string) ([]lint.Code, error) {
	cfg, found, err := r.files.Nearest(filepath.Dir(path), []string{FileName}, nil)
	if err != nil || !found {
		return nil, err
	}
	return cfg.ExcludedFor(path), nil
}
