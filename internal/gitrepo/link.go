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
	marker := filepath.Join(r.Top, MarkerName)
	if !r.Linked {
		return Dirs{Git: marker, Common: marker}, nil
	}
	link, found, err := read(marker)
	if err != nil {
		return Dirs{}, err
	}
	if !found {
		return Dirs{}, fmt.Errorf("reading the git link %s: %w", marker, fs.ErrNotExist)
	}
	dir, err := linkTarget(marker, link)
	if err != nil {
		return Dirs{}, err
	}
	common, shared, err := read(filepath.Join(dir, commonDirFile))
	if err != nil {
		return Dirs{}, err
	}
	if !shared {
		return Dirs{Git: dir, Common: dir}, nil
	}
	return Dirs{Git: dir, Common: resolveLink(dir, common)}, nil
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
