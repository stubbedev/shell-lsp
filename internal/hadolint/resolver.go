package hadolint

import (
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/stubbedev/shell-lsp/internal/bake"
	"github.com/stubbedev/shell-lsp/internal/diskwatch"
	"github.com/stubbedev/shell-lsp/internal/filecache"
	"github.com/stubbedev/shell-lsp/internal/gitrepo"
	"github.com/stubbedev/shell-lsp/internal/platform"
)

var ConfigNames = []string{".hadolint.yaml", ".hadolint.yml"}

var userConfigNames = []string{"hadolint.yaml", "hadolint.yml"}

type Resolver struct {
	files    *filecache.Cache[Config]
	bake     *bake.Resolver
	fallback []string
}

func NewResolver(read filecache.ReadFile, now func() time.Time, recheck time.Duration, fallback []string, git gitrepo.Environment) *Resolver {
	parse := func(path string, data []byte) (Config, error) { return ParseConfig(path, string(data)) }
	return &Resolver{files: filecache.New(read, parse, now, recheck), bake: bake.NewResolver(read, os.ReadDir, now, recheck, git), fallback: fallback}
}

func (r *Resolver) Watch(disk *diskwatch.Watcher) {
	r.files.Watch(disk)
	r.bake.Watch(disk)
}

func ConfigFallback(dirs platform.UserDirs) []string {
	var out []string
	if dir := dirs.ConfigDir(); dir != "" {
		for _, name := range userConfigNames {
			out = append(out, filepath.Join(dir, name))
		}
	}
	if dirs.Home != "" {
		for _, name := range userConfigNames {
			out = append(out, filepath.Join(dirs.Home, ".hadolint", name))
		}
		for _, name := range ConfigNames {
			out = append(out, filepath.Join(dirs.Home, name))
		}
	}
	return out
}

func IsConfigName(path string) bool {
	base := filepath.Base(path)
	return slices.Contains(ConfigNames, base) || slices.Contains(userConfigNames, base) || bake.IsName(path)
}

func (r *Resolver) Nearest(dir string) (Config, bool, error) {
	cfg, found, err := r.files.Nearest(dir, ConfigNames, r.fallback)
	if err != nil {
		return Config{}, false, err
	}
	if cfg.Bake, err = r.bake.Dockerfiles(dir); err != nil {
		return Config{}, false, err
	}
	return cfg, found, nil
}

func (r *Resolver) Load(path string) (Config, bool, error) { return r.files.Load(path) }

func (r *Resolver) Invalidate(path string) {
	r.files.Invalidate(path)
	r.bake.Invalidate(path)
}

func (r *Resolver) InvalidateAll() {
	r.files.InvalidateAll()
	r.bake.InvalidateAll()
}
