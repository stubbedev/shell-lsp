package corpus

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

type Source struct {
	Manifest string
	Cache    string
}

func (s *Source) Register(fs *flag.FlagSet, manifest, cacheName string) {
	fs.StringVar(&s.Manifest, "manifest", manifest, "manifest of the pinned repositories")
	fs.StringVar(&s.Cache, "cache", userCache(cacheName), "directory the pinned repositories are fetched into")
}

func userCache(name string) string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "shell-lsp", name)
}

func (s Source) Repos() ([]Repo, error) {
	if s.Cache == "" {
		return nil, errors.New("no cache directory: pass -cache, or set HOME or XDG_CACHE_HOME")
	}
	return LoadManifest(s.Manifest)
}

func (s Source) Fetch(ctx context.Context) ([]Repo, error) {
	repos, err := s.Repos()
	if err != nil {
		return nil, err
	}
	return repos, Fetch(ctx, repos, s.Cache)
}

func (s Source) Fetched() ([]Repo, error) {
	repos, err := s.Repos()
	if err != nil {
		return nil, err
	}
	return repos, Fetched(repos, s.Cache)
}

func (s Source) Find(name string) (Repo, error) {
	repos, err := s.Repos()
	if err != nil {
		return Repo{}, err
	}
	for _, r := range repos {
		if r.Name != name {
			continue
		}
		if _, err := os.Stat(r.Dir(s.Cache)); err != nil {
			return Repo{}, fmt.Errorf("%s is not fetched into %s; run the fetch subcommand first: %w", name, s.Cache, err)
		}
		return r, nil
	}
	return Repo{}, fmt.Errorf("%s does not list %s", s.Manifest, name)
}
