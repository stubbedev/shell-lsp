package gitrepo

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/stubbedev/shell-lsp/internal/diskwatch"
	"github.com/stubbedev/shell-lsp/internal/filecache"
	"github.com/stubbedev/shell-lsp/internal/gitignore"
	"github.com/stubbedev/shell-lsp/internal/gitlink"
	"github.com/stubbedev/shell-lsp/internal/platform"
)

const (
	MarkerName      = ".git"
	IgnoreFile      = ".gitignore"
	maxIncludeDepth = 10
)

func globalConfigs(u platform.UserDirs) []string {
	var out []string
	if dir := u.ConfigDir(); dir != "" {
		out = append(out, filepath.Join(dir, "git", "config"))
	}
	if u.Home != "" {
		out = append(out, filepath.Join(u.Home, ".gitconfig"))
	}
	return out
}

func defaultExcludes(u platform.UserDirs) string {
	if dir := u.ConfigDir(); dir != "" {
		return filepath.Join(dir, "git", "ignore")
	}
	return ""
}

type Repository struct {
	Top    string
	Linked bool
}

type Ignores struct {
	user    platform.UserDirs
	rules   *filecache.Cache[gitignore.Rules]
	configs *filecache.Cache[config]
	links   *filecache.Cache[string]
}

func ParseRules(_ string, data []byte) (gitignore.Rules, error) {
	return gitignore.Parse(platform.Native.Fold(string(data))), nil
}

func parseLink(_ string, data []byte) (string, error) {
	return strings.TrimSpace(string(data)), nil
}

func NewIgnores(user platform.UserDirs, read filecache.ReadFile, now func() time.Time, recheck time.Duration) *Ignores {
	return &Ignores{
		user:    user,
		rules:   filecache.New(read, ParseRules, now, recheck),
		configs: filecache.New(read, parseConfig, now, recheck),
		links:   filecache.New(read, parseLink, now, recheck),
	}
}

func (g *Ignores) Watch(disk *diskwatch.Watcher) {
	g.rules.Watch(disk)
	g.configs.Watch(disk)
	g.links.Watch(disk)
}

func (g *Ignores) Invalidate(path string) {
	g.rules.Invalidate(path)
	g.configs.Invalidate(path)
	g.links.Invalidate(path)
}

func (g *Ignores) Rules(path string) (gitignore.Rules, error) {
	rules, _, err := g.rules.Load(path)
	return rules, err
}

func (g *Ignores) Base(repo Repository) (*gitignore.Stack, error) {
	common, err := g.commonDir(repo)
	if err != nil {
		return nil, err
	}
	excludes, err := g.excludesFile(repo.Top, common)
	if err != nil {
		return nil, fmt.Errorf("reading core.excludesFile for the repository at %s: %w", repo.Top, err)
	}
	info, err := g.Rules(filepath.Join(common, "info", "exclude"))
	if err != nil {
		return nil, err
	}
	return (*gitignore.Stack)(nil).Push("", excludes).Push("", info), nil
}

func (g *Ignores) commonDir(repo Repository) (string, error) {
	marker := filepath.Join(repo.Top, MarkerName)
	if !repo.Linked {
		return marker, nil
	}
	link, found, err := g.links.Load(marker)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("reading %s: %w", marker, fs.ErrNotExist)
	}
	dir, err := gitlink.Target(marker, link)
	if err != nil {
		return "", err
	}
	common, shared, err := g.links.Load(filepath.Join(dir, gitlink.CommonDirFile))
	if err != nil || !shared {
		return dir, err
	}
	return gitlink.Resolve(dir, common), nil
}

type assignment struct {
	value       string
	origin      string
	conditional string
	set         bool
}

func (g *Ignores) excludesFile(top, common string) (gitignore.Rules, error) {
	var last assignment
	for _, path := range append(globalConfigs(g.user), filepath.Join(common, "config")) {
		if err := g.collect(path, 0, "", &last); err != nil {
			return nil, err
		}
	}
	if last.conditional != "" {
		return nil, fmt.Errorf("it is set at %s, which %s includes under an includeIf condition, and shell-lsp does not evaluate includeIf conditions", last.origin, last.conditional)
	}
	fallback := defaultExcludes(g.user)
	if !last.set {
		return g.rulesAt(fallback)
	}
	if last.value == "" {
		return nil, nil
	}
	named, err := expandPath(last.value, g.user.Home, top)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", last.origin, err)
	}
	rules, found, err := g.rules.Load(named.path)
	if err != nil || found || !named.optional {
		return rules, err
	}
	return g.rulesAt(fallback)
}

func (g *Ignores) rulesAt(path string) (gitignore.Rules, error) {
	if path == "" {
		return nil, nil
	}
	return g.Rules(path)
}

func (g *Ignores) collect(path string, depth int, conditional string, last *assignment) error {
	if depth > maxIncludeDepth {
		return fmt.Errorf("including %s goes past %d nested includes", path, maxIncludeDepth)
	}
	settings, _, err := g.configs.Load(path)
	if err != nil {
		return err
	}
	for _, s := range settings {
		origin := fmt.Sprintf("%s:%d", path, s.line)
		if s.directive == excludesFile {
			*last = assignment{value: s.value, origin: origin, conditional: conditional, set: true}
			continue
		}
		if s.value == "" {
			return fmt.Errorf("%s: %w", origin, errEmptyInclude)
		}
		included, err := expandPath(s.value, g.user.Home, filepath.Dir(path))
		if err != nil {
			return fmt.Errorf("%s: %w", origin, err)
		}
		within := conditional
		if within == "" && s.directive == conditionalInclude {
			within = origin
		}
		if err := g.collect(included.path, depth+1, within, last); err != nil {
			return err
		}
	}
	return nil
}
