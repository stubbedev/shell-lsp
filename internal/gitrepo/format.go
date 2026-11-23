package gitrepo

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/stubbedev/shell-lsp/internal/regularfile"
)

const configFile = "config"

var errFormatVersion = fmt.Errorf("git reads repositories only up to format version %d", maxFormatVersion)

type repositoryFormat struct {
	version        int64
	worktreeConfig bool
	refStorage     string
	objectFormat   string
	bare           bool
	workTree       string
	hasWorkTree    bool
}

func readRepositoryFormat(git gitDirectory, p probe) (repositoryFormat, error) {
	path := filepath.Join(git.Common, configFile)
	settings, found, err := loadConfig(path, p)
	if err != nil || !found {
		return repositoryFormat{}, err
	}
	format, err := readFormat(path, settings)
	if err != nil {
		return repositoryFormat{}, err
	}
	shared := git.shared
	if format.worktreeConfig {
		path := filepath.Join(git.Git, worktreeFile)
		settings, _, err := loadConfig(path, p)
		if err != nil {
			return repositoryFormat{}, err
		}
		for _, s := range settings {
			if err := format.readWorkTree(path, s); err != nil {
				return repositoryFormat{}, err
			}
		}
		shared = false
	}
	if shared {
		format.bare, format.workTree, format.hasWorkTree = false, "", false
	}
	return format, nil
}

func loadConfig(path string, p probe) (config, bool, error) {
	p.track(path)
	data, err := regularfile.ReadConfig(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("reading the git configuration %s: %w", path, err)
	}
	settings, err := parseConfig(path, data)
	return settings, err == nil, err
}

func readFormat(path string, settings config) (repositoryFormat, error) {
	var out repositoryFormat
	versioned := false
	for _, s := range settings {
		switch s.key {
		case formatVersion:
			version, err := parseInt(s.value)
			if err != nil {
				return repositoryFormat{}, fmt.Errorf("%s:%d: core.repositoryFormatVersion: %w", path, s.line, err)
			}
			out.version, versioned = version, true
		case worktreeConfig:
			on, err := parseBool(s.value)
			if err != nil {
				return repositoryFormat{}, fmt.Errorf("%s:%d: extensions.worktreeConfig: %w", path, s.line, err)
			}
			out.worktreeConfig = on
		case refStorage:
			out.refStorage = s.value
		case objectFormat:
			out.objectFormat = s.value
		case workTree, bare:
			if err := out.readWorkTree(path, s); err != nil {
				return repositoryFormat{}, err
			}
		case excludesFile, include, conditionalInclude, remoteURL:
		}
	}
	if !versioned {
		return repositoryFormat{}, nil
	}
	if out.version > maxFormatVersion {
		return repositoryFormat{}, fmt.Errorf("%s: core.repositoryFormatVersion is %d: %w", path, out.version, errFormatVersion)
	}
	return out, nil
}

func (f *repositoryFormat) readWorkTree(path string, s setting) error {
	switch s.key {
	case bare:
		on, err := parseBool(s.value)
		if err != nil {
			return fmt.Errorf("%s:%d: core.bare: %w", path, s.line, err)
		}
		f.bare = on
	case workTree:
		f.workTree, f.hasWorkTree = s.value, true
	case excludesFile, include, conditionalInclude, remoteURL, worktreeConfig, refStorage, formatVersion, objectFormat:
	}
	return nil
}

func (f repositoryFormat) objectIDLength() int {
	if f.version >= 1 && f.objectFormat == sha256Format {
		return sha256Length
	}
	return sha1Length
}

func (f repositoryFormat) usesReftable() bool {
	return f.refStorage != "" && f.refStorage != filesRefStorage
}

func (f repositoryFormat) workTreeOf(gitDir, envWorkTree, implicit string, p probe) (string, bool, error) {
	switch {
	case envWorkTree != "":
		return absoluteWorkTree(envWorkTree, p)
	case f.bare:
		return "", false, nil
	case !f.hasWorkTree:
		return implicit, implicit != "", nil
	case f.workTree == "":
		return "", false, fmt.Errorf("core.worktree of %s is empty, which git cannot change into", gitDir)
	}
	path := filepath.FromSlash(f.workTree)
	if filepath.IsAbs(path) {
		return absoluteWorkTree(filepath.Clean(path), p)
	}
	base, err := filepath.EvalSymlinks(gitDir)
	if err != nil {
		return "", false, fmt.Errorf("resolving the git directory %s to change into core.worktree %q: %w", gitDir, f.workTree, err)
	}
	path = base + string(filepath.Separator) + path
	p.track(path)
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false, fmt.Errorf("core.worktree %q of %s: %w", f.workTree, gitDir, err)
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", false, fmt.Errorf("core.worktree %q of %s: %w", f.workTree, gitDir, err)
	}
	if !info.IsDir() {
		return "", false, fmt.Errorf("core.worktree %q of %s names a %s, not a directory", f.workTree, gitDir, info.Mode().Type())
	}
	return real, true, nil
}

func absoluteWorkTree(path string, p probe) (string, bool, error) {
	p.track(path)
	real, err := filepath.EvalSymlinks(path)
	if missing(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("resolving the work tree %s: %w", path, err)
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", false, fmt.Errorf("resolving the work tree %s: %w", path, err)
	}
	if !info.IsDir() {
		return "", false, nil
	}
	return real, true, nil
}
