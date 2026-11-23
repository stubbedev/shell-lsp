package gitrepo

import (
	"fmt"
	"maps"
	"path/filepath"
	"sync"
	"time"

	"github.com/stubbedev/shell-lsp/internal/diskwatch"
	"github.com/stubbedev/shell-lsp/internal/filecache"
	"github.com/stubbedev/shell-lsp/internal/gitignore"
	"github.com/stubbedev/shell-lsp/internal/platform"
)

const (
	MarkerName = ".git"
	IgnoreFile = ".gitignore"
)

type Ignores struct {
	user      platform.UserDirs
	expand    expander
	scopes    scopes
	scopesErr error
	locate    Locator
	now       func() time.Time
	recheck   time.Duration
	disk      *diskwatch.Watcher
	rules     *filecache.Cache[gitignore.Rules]
	configs   *filecache.Cache[config]
	texts     *filecache.Cache[string]

	mu    sync.Mutex
	repos map[string]located
}

type located struct {
	repo    Repository
	found   bool
	checked time.Time
	stamp   diskwatch.Stamp
}

func ParseRules(_ string, data []byte) (gitignore.Rules, error) {
	return gitignore.Parse(platform.Native.Fold(string(data))), nil
}

func parseText(_ string, data []byte) (string, error) {
	return string(data), nil
}

func NewIgnores(env Environment, read filecache.ReadFile, now func() time.Time, recheck time.Duration) *Ignores {
	scopes, err := newScopes(env)
	return &Ignores{
		user:      env.User,
		expand:    expander{home: env.User.Home, prefix: env.Git.Prefix, userHome: env.Git.UserHome, realPath: filepath.EvalSymlinks},
		scopes:    scopes,
		scopesErr: err,
		locate:    env.Repositories,
		now:       now,
		recheck:   recheck,
		repos:     map[string]located{},
		rules:     filecache.New(read, ParseRules, now, recheck),
		configs:   filecache.New(read, parseConfig, now, recheck),
		texts:     filecache.New(read, parseText, now, recheck),
	}
}

func (g *Ignores) Watch(disk *diskwatch.Watcher) {
	g.disk = disk
	g.rules.Watch(disk)
	g.configs.Watch(disk)
	g.texts.Watch(disk)
}

func (g *Ignores) Invalidate(path string) {
	g.rules.Invalidate(path)
	g.configs.Invalidate(path)
	g.texts.Invalidate(path)
	top, ok := markerParent(path)
	if !ok {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	maps.DeleteFunc(g.repos, func(dir string, _ located) bool { return platform.Native.Within(dir, top) })
}

func markerParent(path string) (string, bool) {
	for dir := filepath.Clean(path); ; dir = filepath.Dir(dir) {
		if filepath.Base(dir) == MarkerName {
			return filepath.Dir(dir), true
		}
		if filepath.Dir(dir) == dir {
			return "", false
		}
	}
}

func (g *Ignores) InvalidateAll() {
	g.rules.InvalidateAll()
	g.configs.InvalidateAll()
	g.texts.InvalidateAll()
	g.mu.Lock()
	defer g.mu.Unlock()
	clear(g.repos)
}

func (g *Ignores) Repository(dir string) (Repository, bool, error) {
	now := g.now()
	g.mu.Lock()
	cached, ok := g.repos[dir]
	g.mu.Unlock()
	if ok && (now.Sub(cached.checked) < g.recheck || g.disk.Unchanged(cached.stamp)) {
		return cached.repo, cached.found, nil
	}
	stamp := g.disk.Current()
	repo, found, err := g.locate.find(dir, probe{track: func(path string) { stamp = stamp.And(g.disk.Track(path)) }, device: platform.DeviceOf})
	if err != nil {
		return Repository{}, false, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.repos[dir] = located{repo: repo, found: found, checked: now, stamp: stamp}
	return repo, found, nil
}

func (g *Ignores) Rules(path string) (gitignore.Rules, error) {
	rules, _, err := g.rules.Load(path)
	return rules, err
}

func (g *Ignores) Base(repo Repository) (*gitignore.Stack, error) {
	excludes, err := g.excludesFile(repo)
	if err != nil {
		return nil, fmt.Errorf("reading core.excludesFile for the repository at %s: %w", repo.Top, err)
	}
	info, err := g.Rules(filepath.Join(repo.Dirs.Common, "info", "exclude"))
	if err != nil {
		return nil, err
	}
	return (*gitignore.Stack)(nil).Push("", excludes).Push("", info), nil
}

func (g *Ignores) sources(repo Repository) []source {
	var out []source
	if g.scopes.system != "" {
		out = append(out, source{path: within(repo.Top, g.scopes.system)})
	}
	for _, path := range g.scopes.globals {
		out = append(out, source{path: within(repo.Top, path)})
	}
	out = append(out, source{path: filepath.Join(repo.Dirs.Common, configFile)})
	if repo.format.worktreeConfig {
		out = append(out, source{path: filepath.Join(repo.Dirs.Git, worktreeFile)})
	}
	return append(out, g.scopes.commandLine...)
}

func within(top, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(top, path)
}

func (g *Ignores) excludesFile(repo Repository) (gitignore.Rules, error) {
	if g.scopesErr != nil {
		return nil, g.scopesErr
	}
	w := walker{g: g, repo: repo, sources: g.sources(repo)}
	if err := w.run(); err != nil {
		return nil, err
	}
	fallback := g.defaultExcludes()
	named := w.last.named
	switch {
	case !w.last.set, named.path == "" && named.optional:
		return g.rulesAt(fallback)
	case named.path == "":
		return nil, nil
	}
	rules, found, err := g.rules.Load(named.path)
	if err != nil || found || !named.optional {
		return rules, err
	}
	return g.rulesAt(fallback)
}

func (g *Ignores) defaultExcludes() string {
	if dir := g.user.ConfigDir(); dir != "" {
		return filepath.Join(dir, "git", "ignore")
	}
	return ""
}

func (g *Ignores) rulesAt(path string) (gitignore.Rules, error) {
	if path == "" {
		return nil, nil
	}
	return g.Rules(path)
}
