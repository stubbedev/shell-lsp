package corpus

import (
	"bytes"
	"cmp"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
)

const maxFileSize = 1 << 20

type Script struct {
	Path string
	Text string
	dialect.Detection
}

type Listing struct {
	Scripts []Script
	Skipped int
}

func Scripts(repos []Repo, cache string) (Listing, error) {
	var out Listing
	for _, r := range repos {
		if r.Kind != KindScripts {
			continue
		}
		if err := walk(r.Dir(cache), r.Name, &out); err != nil {
			return Listing{}, fmt.Errorf("listing the scripts of %s: %w", r.Name, err)
		}
	}
	return out.sorted(), nil
}

func ScriptsUnder(roots []string) (Listing, error) {
	var out Listing
	for _, root := range roots {
		if err := walk(root, "", &out); err != nil {
			return Listing{}, fmt.Errorf("listing the scripts under %s: %w", root, err)
		}
	}
	return out.sorted(), nil
}

func (l Listing) sorted() Listing {
	slices.SortFunc(l.Scripts, func(a, b Script) int { return cmp.Compare(a.Path, b.Path) })
	l.Scripts = slices.CompactFunc(l.Scripts, func(a, b Script) bool { return a.Path == b.Path })
	return l
}

func walk(root, prefix string, out *Listing) error {
	return filepath.WalkDir(root, func(file string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() == 0 || info.Size() > maxFileSize {
			return nil
		}
		raw, err := os.ReadFile(file) //nolint:gosec
		if err != nil {
			return err
		}
		name, err := displayName(root, prefix, file)
		if err != nil {
			return err
		}
		detection, ok := Classify(name, string(raw))
		if !ok {
			return nil
		}
		if !utf8.Valid(raw) || bytes.ContainsRune(raw, '\r') {
			out.Skipped++
			return nil
		}
		out.Scripts = append(out.Scripts, Script{Path: name, Text: string(raw), Detection: detection})
		return nil
	})
}

func displayName(root, prefix, file string) (string, error) {
	if prefix == "" {
		return file, nil
	}
	rel, err := filepath.Rel(root, file)
	if err != nil {
		return "", err
	}
	return prefix + "/" + filepath.ToSlash(rel), nil
}

func Classify(name, text string) (dialect.Detection, bool) {
	if interpreter, ok := dialect.Interpreter(text); ok {
		if _, known := dialect.FromInterpreter(interpreter); !known {
			return dialect.Detection{}, false
		}
	}
	found := dialect.Resolve(nil, dialect.Input{URI: fileuri.FromPath("/" + strings.TrimPrefix(name, "/")), Text: text})
	return found, found.Source != dialect.SourceDefault && !found.Bats()
}
