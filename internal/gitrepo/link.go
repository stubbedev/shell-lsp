package gitrepo

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/regularfile"
)

const (
	gitFilePrefix = "gitdir: "
	commonDirFile = "commondir"
	maxGitFile    = 1 << 20
	lineEnds      = "\r\n"
)

type ReadText func(path string) (text string, found bool, err error)

type Dirs struct {
	Git    string
	Common string
}

func readGitFile(path, commonDir string, p probe) (gitDirectory, error) {
	data, _, err := regularfile.Read(path, maxGitFile)
	if err != nil {
		return gitDirectory{}, fmt.Errorf("reading the gitdir: file %s: %w", path, err)
	}
	target, ok := strings.CutPrefix(string(data), gitFilePrefix)
	if !ok {
		return gitDirectory{}, fmt.Errorf("%s does not start with %q, so it names no git directory", path, gitFilePrefix)
	}
	target = strings.TrimRight(target, lineEnds)
	if target == "" {
		return gitDirectory{}, fmt.Errorf("%s names no path after %q", path, gitFilePrefix)
	}
	gitDir := resolveLink(filepath.Dir(path), target)
	git, valid, err := openGitDirectory(gitDir, commonDir, p)
	if err != nil {
		return gitDirectory{}, err
	}
	if !valid {
		return gitDirectory{}, fmt.Errorf("%s points to %s, which is not a git repository: it lacks a valid HEAD, objects or refs", path, gitDir)
	}
	return git, nil
}

func commonDirOf(gitDir, commonDir string, p probe) (gitDirectory, error) {
	if commonDir != "" {
		return gitDirectory{Git: gitDir, Common: commonDir, shared: true}, nil
	}
	path := filepath.Join(gitDir, commonDirFile)
	p.track(path)
	data, _, err := regularfile.Read(path, maxGitFile)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return gitDirectory{Git: gitDir, Common: gitDir}, nil
	case err != nil:
		return gitDirectory{}, fmt.Errorf("reading %s: %w", path, err)
	case len(data) == 0:
		return gitDirectory{}, fmt.Errorf("%s is empty, so it names no common git directory", path)
	}
	return gitDirectory{Git: gitDir, Common: resolveLink(gitDir, strings.TrimRight(string(data), lineEnds)), shared: true}, nil
}

func resolveLink(base, path string) string {
	path = filepath.FromSlash(path)
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(base, path)
}
