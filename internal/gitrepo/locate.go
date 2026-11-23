package gitrepo

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/stubbedev/shell-lsp/internal/platform"
)

const (
	gitDirVariable    = "GIT_DIR"
	workTreeVariable  = "GIT_WORK_TREE"
	commonDirVariable = "GIT_COMMON_DIR"
)

var locationVariables = []string{gitDirVariable, workTreeVariable, commonDirVariable}

type Repository struct {
	Top       string
	marker    string
	linked    bool
	commonDir string
}

func Discover(dir string) (Repository, bool, error) {
	return discover(dir, func(string) {})
}

func discover(dir string, track func(path string)) (Repository, bool, error) {
	for current := dir; ; current = filepath.Dir(current) {
		marker := filepath.Join(current, MarkerName)
		track(marker)
		repo, found, err := repositoryAt(current, marker)
		if err != nil || found {
			return repo, found, err
		}
		if filepath.Dir(current) == current {
			return Repository{}, false, nil
		}
	}
}

func repositoryAt(top, marker string) (Repository, bool, error) {
	info, err := os.Stat(marker)
	switch {
	case err == nil && info.IsDir():
		return Repository{Top: top, marker: marker}, true, nil
	case err == nil && info.Mode().IsRegular():
		return Repository{Top: top, marker: marker, linked: true}, true, nil
	case err == nil, errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOTDIR), errors.Is(err, fs.ErrPermission):
		return Repository{}, false, nil
	}
	return Repository{}, false, fmt.Errorf("looking for a git repository at %s: %w", marker, err)
}

type Locator struct {
	process *processLocation
	err     error
}

type processLocation struct {
	cwd       string
	gitDir    string
	workTree  string
	commonDir string
}

func Discovery() Locator { return Locator{} }

func (p Process) Locate(cwd string) Locator {
	location := processLocation{cwd: cwd}
	set := false
	for _, v := range []struct {
		name string
		path *string
	}{{gitDirVariable, &location.gitDir}, {workTreeVariable, &location.workTree}, {commonDirVariable, &location.commonDir}} {
		value, ok := p.Variables[v.name]
		if !ok {
			continue
		}
		if value == "" {
			return Locator{err: fmt.Errorf("%s is set to the empty string, which git does not accept as a path", v.name)}
		}
		set = true
		*v.path = filepath.Clean(value)
		if !filepath.IsAbs(value) {
			*v.path = filepath.Join(cwd, value)
		}
	}
	if !set {
		return Discovery()
	}
	return Locator{process: &location}
}

func (l Locator) find(dir string, track func(path string)) (Repository, bool, error) {
	if l.err != nil {
		return Repository{}, false, l.err
	}
	if l.process == nil {
		return discover(dir, track)
	}
	repo, err := l.process.repository(track)
	if err != nil {
		return Repository{}, false, err
	}
	if !platform.Native.Within(dir, repo.Top) {
		return Repository{}, false, nil
	}
	return repo, true, nil
}

func (p processLocation) repository(track func(path string)) (Repository, error) {
	repo, err := p.gitDirectory(track)
	if err != nil {
		return Repository{}, err
	}
	if p.workTree != "" {
		repo.Top = p.workTree
	}
	repo.commonDir = p.commonDir
	return repo, nil
}

func (p processLocation) gitDirectory(track func(path string)) (Repository, error) {
	if p.gitDir == "" {
		repo, found, err := discover(p.cwd, track)
		if err != nil {
			return Repository{}, err
		}
		if !found {
			return Repository{}, fmt.Errorf("%s or %s is set, but %s is not in a git repository", workTreeVariable, commonDirVariable, p.cwd)
		}
		return repo, nil
	}
	track(p.gitDir)
	info, err := os.Stat(p.gitDir)
	switch {
	case err != nil:
		return Repository{}, fmt.Errorf("%s=%s is not a git repository: %w", gitDirVariable, p.gitDir, err)
	case info.IsDir():
		return Repository{Top: p.cwd, marker: p.gitDir}, nil
	case info.Mode().IsRegular():
		return Repository{Top: p.cwd, marker: p.gitDir, linked: true}, nil
	}
	return Repository{}, fmt.Errorf("%s=%s is a %s, not a git repository", gitDirVariable, p.gitDir, info.Mode().Type())
}
