package corpus

import (
	"cmp"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/host"
)

type HostFile struct {
	Repo     string
	Root     string
	Path     string
	File     string
	Language host.Language
}

func Hosts(repos []Repo, cache string) ([]HostFile, error) {
	var out []HostFile
	for _, r := range repos {
		if r.Kind != KindHosts {
			continue
		}
		root := r.Dir(cache)
		err := filepath.WalkDir(root, func(file string, d fs.DirEntry, err error) error {
			if err != nil || !d.Type().IsRegular() {
				return err
			}
			language, ok := host.FromName(file)
			if !ok || language == host.Nix {
				return nil
			}
			name, err := displayName(root, r.Name, file)
			if err != nil {
				return err
			}
			out = append(out, HostFile{Repo: r.Name, Root: root, Path: name, File: file, Language: language})
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("listing the justfiles and Makefiles of %s: %w", r.Name, err)
		}
	}
	slices.SortFunc(out, func(a, b HostFile) int { return cmp.Compare(a.Path, b.Path) })
	return out, nil
}
