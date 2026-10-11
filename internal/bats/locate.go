package bats

import (
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"strings"
)

const command = "bats"

var projectLocal = []string{"test/bats/bin/bats", "node_modules/.bin/bats", "test/libs/bats/bin/bats"}

func Locate(dir string) (string, error) {
	candidates := make([]string, 0, len(projectLocal)+1)
	for _, rel := range projectLocal {
		candidates = append(candidates, filepath.Join(dir, filepath.FromSlash(rel)))
	}
	for _, candidate := range append(candidates, command) {
		path, found, err := lookPath(candidate)
		if err != nil || found {
			return path, err
		}
	}
	return "", fmt.Errorf("bats not found: looked for %s under %s, then for %s on PATH", strings.Join(projectLocal, ", "), dir, command)
}

func lookPath(name string) (string, bool, error) {
	path, err := exec.LookPath(name)
	switch {
	case err == nil:
		return path, true, nil
	case errors.Is(err, exec.ErrNotFound), errors.Is(err, fs.ErrNotExist):
		return "", false, nil
	}
	return "", false, fmt.Errorf("checking %s for a bats executable: %w", name, err)
}
