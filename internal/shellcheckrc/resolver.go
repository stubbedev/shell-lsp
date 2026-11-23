package shellcheckrc

import (
	"path/filepath"
	"slices"
	"time"

	"github.com/stubbedev/shell-lsp/internal/diskwatch"
	"github.com/stubbedev/shell-lsp/internal/filecache"
	"github.com/stubbedev/shell-lsp/internal/platform"
)

type Resolver struct {
	files    *filecache.Cache[File]
	fallback []string
}

func NewResolver(read filecache.ReadFile, now func() time.Time, recheck time.Duration, fallback []string) *Resolver {
	parse := func(path string, data []byte) (File, error) { return Parse(path, string(data)) }
	return &Resolver{files: filecache.New(read, parse, now, recheck), fallback: fallback}
}

func (r *Resolver) Watch(disk *diskwatch.Watcher) { r.files.Watch(disk) }

func Fallback(dirs platform.UserDirs) []string {
	var out []string
	if dir := dirs.ConfigDir(); dir != "" {
		out = append(out, filepath.Join(dir, "shellcheckrc"))
	}
	if dirs.Home != "" {
		out = append(out, filepath.Join(dirs.Home, FileName))
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
