package gitrepo

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

const (
	linkPrefix    = "gitdir:"
	commonDirFile = "commondir"
)

type ReadText func(path string) (text string, found bool, err error)

type Dirs struct {
	Git    string
	Common string
}

func (r Repository) Dirs(read ReadText) (Dirs, error) {
	git, err := r.gitDir(read)
	if err != nil {
		return Dirs{}, err
	}
	if r.commonDir != "" {
		return Dirs{Git: git, Common: r.commonDir}, nil
	}
	common, shared, err := read(filepath.Join(git, commonDirFile))
	if err != nil {
		return Dirs{}, err
	}
	if !shared {
		return Dirs{Git: git, Common: git}, nil
	}
	return Dirs{Git: git, Common: resolveLink(git, common)}, nil
}

func (r Repository) gitDir(read ReadText) (string, error) {
	if !r.linked {
		return r.marker, nil
	}
	link, found, err := read(r.marker)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("reading the git link %s: %w", r.marker, fs.ErrNotExist)
	}
	return linkTarget(r.marker, link)
}

func linkTarget(dotGit, text string) (string, error) {
	target, ok := strings.CutPrefix(strings.TrimSpace(text), linkPrefix)
	if !ok {
		return "", fmt.Errorf("%s does not name a git directory", dotGit)
	}
	return resolveLink(filepath.Dir(dotGit), target), nil
}

func resolveLink(base, path string) string {
	path = filepath.FromSlash(strings.TrimSpace(path))
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(base, path)
}
