package hadolint

import (
	"path/filepath"
	"slices"
	"time"

	"github.com/stubbedev/shell-lsp/internal/filecache"
)

var ConfigNames = []string{".hadolint.yaml", ".hadolint.yml"}

var userConfigNames = []string{"hadolint.yaml", "hadolint.yml"}

type Resolver struct {
	files    *filecache.Cache[Config]
	fallback []string
}

func NewResolver(read filecache.ReadFile, now func() time.Time, recheck time.Duration, fallback []string) *Resolver {
	parse := func(path string, data []byte) (Config, error) { return ParseConfig(path, string(data)) }
	return &Resolver{files: filecache.New(read, parse, now, recheck), fallback: fallback}
}

func ConfigFallback(home, configHome string) []string {
	var out []string
	if configHome == "" && home != "" {
		configHome = filepath.Join(home, ".config")
	}
	if configHome != "" {
		for _, name := range userConfigNames {
			out = append(out, filepath.Join(configHome, name))
		}
	}
	if home != "" {
		for _, name := range userConfigNames {
			out = append(out, filepath.Join(home, ".hadolint", name))
		}
		for _, name := range ConfigNames {
			out = append(out, filepath.Join(home, name))
		}
	}
	return out
}

func IsConfigName(path string) bool {
	base := filepath.Base(path)
	return slices.Contains(ConfigNames, base) || slices.Contains(userConfigNames, base)
}

func (r *Resolver) Nearest(dir string) (Config, bool, error) {
	return r.files.Nearest(dir, ConfigNames, r.fallback)
}

func (r *Resolver) Load(path string) (Config, bool, error) { return r.files.Load(path) }

func (r *Resolver) Invalidate(path string) { r.files.Invalidate(path) }
