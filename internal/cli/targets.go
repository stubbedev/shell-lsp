package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
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

func collectTargets(dir string, operands []string, walkKeeps func(path string) bool) ([]target, []string) {
	var out []target
	var failures []string
	seen := map[string]bool{}
	add := func(t target) {
		key := platform.Native.Fold(t.path)
		if !seen[key] {
			seen[key] = true
			out = append(out, t)
		}
	}
	for _, operand := range operands {
		path := absolute(dir, operand)
		info, err := os.Stat(path)
		switch {
		case err != nil:
			failures = append(failures, fmt.Sprintf("%s: %v", operand, err))
		case info.IsDir():
			found, walkFailures := walkTargets(operand, path, walkKeeps)
			failures = append(failures, walkFailures...)
			for _, t := range found {
				add(t)
			}
		default:
			add(target{name: operand, path: path})
		}
	}
	return out, failures
}

func walkTargets(operand, dir string, keeps func(path string) bool) ([]target, []string) {
	walker := workspace.New(func(string) (*workspace.File, bool) { return nil, false }, workspace.Detect)
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
	return &workspace.File{Path: path, URI: uri, Doc: doc, Tree: parser.ParseEdit(nil, doc.Text(), resolved.Shell)}, resolved, nil
}
