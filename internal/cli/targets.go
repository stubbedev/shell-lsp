package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/parallel"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/platform"
	"github.com/stubbedev/shell-lsp/internal/project"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

type target struct {
	name   string
	path   string
	walked bool
}

func absolute(dir, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(dir, path)
}

type collected struct {
	target
	text func() ([]byte, error)
}

type targetSelection struct {
	walkKeeps   func(path string) bool
	forcedShell bool
	read        func(path string) ([]byte, error)
}

func readDisk(path string) ([]byte, error) {
	raw, _, err := workspace.ReadDisk(path)
	return raw, err
}

func collectTargets(dir string, git platform.UserDirs, operands []string, sel targetSelection) ([]collected, []string) {
	var out []collected
	var failures []string
	seen := map[string]bool{}
	add := func(t target) {
		key := platform.Native.Fold(t.path)
		if !seen[key] {
			seen[key] = true
			out = append(out, collected{target: t, text: sync.OnceValues(func() ([]byte, error) { return sel.read(t.path) })})
		}
	}
	for _, operand := range operands {
		path := absolute(dir, operand)
		info, err := os.Stat(path)
		switch {
		case err != nil:
			failures = append(failures, fmt.Sprintf("%s: %v", operand, err))
		case info.IsDir():
			found, walkFailures := walkTargets(operand, path, git, sel.walkKeeps)
			failures = append(failures, walkFailures...)
			for _, t := range found {
				add(t)
			}
		default:
			add(target{name: operand, path: path})
		}
	}
	if sel.forcedShell {
		return out, failures
	}
	kept, skipFailures := shellOperands(out)
	return kept, append(failures, skipFailures...)
}

func shellOperands(found []collected) ([]collected, []string) {
	shell := make([]bool, len(found))
	errs := make([]error, len(found))
	parallel.For(runtime.GOMAXPROCS(0), len(found), func(i int) {
		shell[i], errs[i] = found[i].shell()
	})
	kept := make([]collected, 0, len(found))
	var failures []string
	for i, o := range found {
		switch {
		case errs[i] != nil:
			failures = append(failures, fmt.Sprintf("%s: %v", o.name, errs[i]))
		case shell[i]:
			kept = append(kept, o)
		}
	}
	return kept, failures
}

func (o collected) shell() (bool, error) {
	if o.walked || workspace.ShellByName(o.path) {
		return true, nil
	}
	raw, err := o.text()
	if err != nil {
		return false, err
	}
	return workspace.NamesShell(string(raw)), nil
}

func walkTargets(operand, dir string, git platform.UserDirs, keeps func(path string) bool) ([]target, []string) {
	walker := workspace.New(func(string) (*workspace.File, bool) { return nil, false }, workspace.Detect, git)
	walker.SetRoots([]string{dir})
	paths, err := walker.AllShellFiles(context.Background())
	if err != nil {
		return nil, []string{fmt.Sprintf("%s: %v", operand, err)}
	}
	var out []target
	var failures []string
	for _, path := range paths {
		if !keeps(path) {
			continue
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		out = append(out, target{name: filepath.Join(operand, rel), path: path, walked: true})
	}
	return out, failures
}

func parseText(path, raw string, resolve func(dialect.Input) (project.Resolution, error)) (*workspace.File, project.Resolution, error) {
	uri := ""
	if path != "" {
		uri = fileuri.FromPath(path)
	}
	doc := docstore.NewDocument(uri, "", 0, raw)
	resolved, err := resolve(dialect.Input{URI: uri, Text: doc.Text()})
	if err != nil {
		return nil, project.Resolution{}, err
	}
	return &workspace.File{Path: path, URI: uri, Doc: doc, Tree: parser.ParseEdit(nil, doc.Text(), resolved.Shell), Detection: resolved.Detection}, resolved, nil
}
