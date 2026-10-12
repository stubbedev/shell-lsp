package bats

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/platform"
)

const command = "bats"

var projectLocal = []string{"test/bats/bin/bats", "node_modules/.bin/bats", "test/libs/bats/bin/bats"}

type Command struct {
	Program string
	Args    []string
}

func (c Command) String() string {
	return strings.Join(append([]string{c.Program}, c.Args...), " ")
}

type host struct {
	flavour    platform.Flavour
	pathDirs   []string
	systemRoot string
	lookPath   func(string) (string, error)
	isFile     func(string) bool
}

type Environment struct {
	Path       []string
	PathExt    []string
	SystemRoot string
}

func Locate(dir string, env Environment) (Command, error) {
	return host{
		flavour:    platform.Native,
		pathDirs:   env.Path,
		systemRoot: env.SystemRoot,
		lookPath: func(name string) (string, error) {
			if found, ok := platform.Native.LookPath(name, env.Path, env.PathExt, os.Stat); ok {
				return found, nil
			}
			if filepath.IsAbs(name) && isFile(name) {
				return "", fmt.Errorf("%s is not executable: %w", name, fs.ErrPermission)
			}
			return "", fmt.Errorf("looking for %s: %w", name, exec.ErrNotFound)
		},
		isFile: isFile,
	}.locate(dir)
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func (h host) locate(dir string) (Command, error) {
	for _, rel := range projectLocal {
		candidate := filepath.Join(dir, filepath.FromSlash(rel))
		if cmd, found, err := h.runner(candidate, []string{candidate}); err != nil || found {
			return cmd, err
		}
	}
	scripts := make([]string, 0, len(h.pathDirs))
	for _, d := range h.pathDirs {
		if d != "" {
			scripts = append(scripts, filepath.Join(d, command))
		}
	}
	if cmd, found, err := h.runner(command, scripts); err != nil || found {
		return cmd, err
	}
	return Command{}, fmt.Errorf("bats not found: looked for %s under %s, then for %s on PATH", strings.Join(projectLocal, ", "), dir, command)
}

func (h host) runner(name string, scripts []string) (Command, bool, error) {
	path, err := h.lookPath(name)
	switch {
	case err == nil:
		return Command{Program: path}, true, nil
	case !errors.Is(err, exec.ErrNotFound) && !errors.Is(err, fs.ErrNotExist):
		return Command{}, false, fmt.Errorf("checking %s for a bats executable: %w", name, err)
	case h.flavour == platform.POSIX:
		return Command{}, false, nil
	}
	for _, script := range scripts {
		if h.isFile(script) {
			cmd, err := h.throughBash(script)
			return cmd, err == nil, err
		}
	}
	return Command{}, false, nil
}

func (h host) throughBash(script string) (Command, error) {
	wsl := h.flavour.Fold(h.flavour.Join(h.systemRoot, "System32"))
	for _, d := range h.pathDirs {
		if d == "" || h.flavour.Fold(h.flavour.Clean(d)) == wsl {
			continue
		}
		if bash := filepath.Join(d, "bash.exe"); h.isFile(bash) {
			return Command{Program: bash, Args: []string{strings.ReplaceAll(script, `\`, "/")}}, nil
		}
	}
	return Command{}, fmt.Errorf("found the bats script %s, but running it on Windows needs the bash.exe of Git for Windows or MSYS2 on PATH; the WSL bash in %s cannot run it", script, h.flavour.Join(h.systemRoot, "System32"))
}
