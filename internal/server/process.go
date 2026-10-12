package server

import (
	"errors"
	"os"
	"slices"
	"strings"
)

type Process struct {
	Path       string
	ManPath    string
	PathExt    string
	Home       string
	ConfigHome string
	WorkDir    string
	Environ    []string
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
	return Process{
		Path:       os.Getenv("PATH"),
		ManPath:    os.Getenv("MANPATH"),
		PathExt:    os.Getenv("PATHEXT"),
		Home:       home,
		ConfigHome: os.Getenv("XDG_CONFIG_HOME"),
		WorkDir:    workDir,
		Environ:    os.Environ(),
	}
}

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
