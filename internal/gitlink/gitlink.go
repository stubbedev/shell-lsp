package gitlink

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	prefix        = "gitdir:"
	CommonDirFile = "commondir"
)

func Target(dotGit, text string) (string, error) {
	target, ok := strings.CutPrefix(strings.TrimSpace(text), prefix)
	if !ok {
		return "", fmt.Errorf("%s does not name a git directory", dotGit)
	}
	return Resolve(filepath.Dir(dotGit), target), nil
}

func Resolve(base, path string) string {
	path = filepath.FromSlash(strings.TrimSpace(path))
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(base, path)
}
