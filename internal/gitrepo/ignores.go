package gitrepo

import (
	"fmt"
	"path/filepath"
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

type Repository struct {
	Top    string
	Linked bool
}

type Ignores struct {
	user      platform.UserDirs
	expand    expander
	scopes    scopes
	scopesErr error
	rules     *filecache.Cache[gitignore.Rules]
	configs   *filecache.Cache[config]
	texts     *filecache.Cache[string]
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
		rules:     filecache.New(read, ParseRules, now, recheck),
		configs:   filecache.New(read, parseConfig, now, recheck),
		texts:     filecache.New(read, parseText, now, recheck),
	}
}

func (g *Ignores) Watch(disk *diskwatch.Watcher) {
	g.rules.Watch(disk)
	g.configs.Watch(disk)
	g.texts.Watch(disk)
}

func (g *Ignores) Invalidate(path string) {
	g.rules.Invalidate(path)
	g.configs.Invalidate(path)
	g.texts.Invalidate(path)
}

func (g *Ignores) InvalidateAll() {
	g.rules.InvalidateAll()
	g.configs.InvalidateAll()
	g.texts.InvalidateAll()
}

func (g *Ignores) Rules(path string) (gitignore.Rules, error) {
	rules, _, err := g.rules.Load(path)
	return rules, err
}

func (g *Ignores) Base(repo Repository) (*gitignore.Stack, error) {
	dirs, err := repo.Dirs(g.texts.Load)
	if err != nil {
		return nil, err
	}
	excludes, err := g.excludesFile(repo.Top, dirs)
	if err != nil {
		return nil, fmt.Errorf("reading core.excludesFile for the repository at %s: %w", repo.Top, err)
	}
	info, err := g.Rules(filepath.Join(dirs.Common, "info", "exclude"))
	if err != nil {
		return nil, err
	}
	return (*gitignore.Stack)(nil).Push("", excludes).Push("", info), nil
}

func (g *Ignores) sources(top string, dirs Dirs, format repositoryFormat) []source {
	var out []source
	if g.scopes.system != "" {
		out = append(out, source{path: within(top, g.scopes.system)})
	}
	for _, path := range g.scopes.globals {
		out = append(out, source{path: within(top, path)})
	}
	out = append(out, source{path: filepath.Join(dirs.Common, "config")})
	if format.worktreeConfig {
		out = append(out, source{path: filepath.Join(dirs.Git, worktreeFile)})
	}
	return append(out, g.scopes.commandLine...)
}

func within(top, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(top, path)
}

func (g *Ignores) excludesFile(top string, dirs Dirs) (gitignore.Rules, error) {
	if g.scopesErr != nil {
		return nil, g.scopesErr
	}
	repoConfig := filepath.Join(dirs.Common, "config")
	settings, _, err := g.configs.Load(repoConfig)
	if err != nil {
		return nil, err
	}
	format, err := readFormat(repoConfig, settings)
	if err != nil {
		return nil, err
	}
	w := walker{g: g, top: top, dirs: dirs, format: format, sources: g.sources(top, dirs, format)}
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
