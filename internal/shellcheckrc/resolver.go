package shellcheckrc

import (
	"path/filepath"
	"slices"
	"time"

	"github.com/stubbedev/shell-lsp/internal/filecache"
)

type Resolver struct {
	files    *filecache.Cache[File]
	fallback []string
}

func NewResolver(read filecache.ReadFile, now func() time.Time, recheck time.Duration, fallback []string) *Resolver {
	parse := func(path string, data []byte) (File, error) { return Parse(path, string(data)) }
	return &Resolver{files: filecache.New(read, parse, now, recheck), fallback: fallback}
}

func Fallback(home, configHome string) []string {
	var out []string
	if configHome == "" && home != "" {
		configHome = filepath.Join(home, ".config")
	}
	if configHome != "" {
		out = append(out, filepath.Join(configHome, "shellcheckrc"))
	}
	if home != "" {
		out = append(out, filepath.Join(home, FileName))
	}
	return out
}

func IsName(path string) bool {
	return slices.Contains(Names, filepath.Base(path))
}

func (r *Resolver) Nearest(dir string) (File, bool, error) {
	return r.files.Nearest(dir, Names, r.fallback)
}

func (r *Resolver) Load(path string) (File, bool, error) {
	return r.files.Load(path)
}

func (r *Resolver) Invalidate(path string) {
	r.files.Invalidate(path)
}
