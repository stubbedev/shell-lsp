package server

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/gitrepo"
	"github.com/stubbedev/shell-lsp/internal/platform"
)

type Process struct {
	Path       string
	ManPath    string
	PathExt    string
	Home       string
	ConfigHome string
	WorkDir    string
	SystemRoot string
	Environ    []string
	Git        gitrepo.Process
}

func CurrentProcess() Process {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	workDir, err := os.Getwd()
	if err != nil {
		workDir = ""
	}
	p := Process{
		Path:       os.Getenv("PATH"),
		ManPath:    os.Getenv("MANPATH"),
		PathExt:    os.Getenv("PATHEXT"),
		Home:       home,
		ConfigHome: os.Getenv("XDG_CONFIG_HOME"),
		WorkDir:    workDir,
		SystemRoot: os.Getenv("SystemRoot"),
		Environ:    os.Environ(),
	}
	p.Git = gitrepo.CurrentProcess(p.Environ, p.PathDirs(), p.pathExt())
	return p
}

func (p Process) dirs() platform.UserDirs {
	return platform.UserDirs{Home: p.Home, ConfigHome: p.ConfigHome}
}

func (p Process) git() gitrepo.Environment {
	return gitrepo.Environment{User: p.dirs(), Git: p.Git}
}

func (p Process) PathDirs() []string { return filepath.SplitList(p.Path) }

func (p Process) pathExt() []string { return platform.Native.PathExt(p.PathExt) }

var errNoWorkDir = errors.New("the working directory of shell-lsp is unknown")

func (p Process) workDir() (string, error) {
	if p.WorkDir == "" {
		return "", errNoWorkDir
	}
	return p.WorkDir, nil
}

func (p Process) environmentNames() []string {
	names := make([]string, 0, len(p.Environ))
	for _, kv := range p.Environ {
		if name, _, ok := strings.Cut(kv, "="); ok && name != "" {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}
