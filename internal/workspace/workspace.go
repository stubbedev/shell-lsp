package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

const (
	maxFileSize  = 4 << 20
	maxScanFiles = 20000
	shebangProbe = 256
)

var shellExtensions = []string{".sh", ".bash", ".zsh", ".ksh", ".dash", ".bats"}

var rcFiles = map[string]bool{
	".bashrc": true, ".bash_profile": true, ".bash_login": true, ".bash_logout": true, ".bash_aliases": true,
	".profile": true, ".zshrc": true, ".zprofile": true, ".zshenv": true, ".zlogin": true, ".zlogout": true,
	".kshrc": true, ".mkshrc": true,
}

var skippedDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true, "node_modules": true, ".direnv": true, ".venv": true,
	"vendor": true, "__pycache__": true, ".cache": true, "result": true,
}

type File struct {
	Path string
	URI  string
	Doc  *docstore.Document
	Tree *parser.Tree
}

type Overlay func(uri string) (*File, bool)

type Workspace struct {
	overlay  Overlay
	override func(path string) *dialect.Dialect

	mu    sync.Mutex
	roots []string
	files map[string]diskEntry
}

type diskEntry struct {
	size    int64
	modTime time.Time
	file    *File
}

func New(overlay Overlay, override func(path string) *dialect.Dialect) *Workspace {
	return &Workspace{overlay: overlay, override: override, files: map[string]diskEntry{}}
}

func (w *Workspace) SetRoots(roots []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.roots = slices.Clone(roots)
}

func (w *Workspace) Roots() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.roots)
}

func (w *Workspace) Invalidate(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.files, path)
}

func (w *Workspace) InvalidateAll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	clear(w.files)
}

func (w *Workspace) Load(path string) (*File, error) {
	uri := fileuri.FromPath(path)
	if f, ok := w.overlay(uri); ok {
		return f, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", path, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("loading %s: %w", path, fs.ErrInvalid)
	}
	if info.Size() > maxFileSize {
		return nil, fmt.Errorf("loading %s: %d bytes exceeds the %d byte limit", path, info.Size(), maxFileSize)
	}
	w.mu.Lock()
	cached, ok := w.files[path]
	w.mu.Unlock()
	if ok && cached.size == info.Size() && cached.modTime.Equal(info.ModTime()) {
		return cached.file, nil
	}
	raw, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", path, err)
	}
	doc := docstore.NewDocument(uri, "", 0, string(raw))
	d := dialect.Detect(w.override(path), dialect.Input{URI: uri, Text: doc.Text()})
	file := &File{Path: path, URI: uri, Doc: doc, Tree: parser.Parse(doc.Text(), d)}
	w.mu.Lock()
	w.files[path] = diskEntry{size: info.Size(), modTime: info.ModTime(), file: file}
	w.mu.Unlock()
	return file, nil
}

func (w *Workspace) ShellFiles(ctx context.Context) ([]string, error) {
	var out []string
	for _, root := range w.Roots() {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if err != nil {
				if errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			if entry.IsDir() {
				if path != root && skippedDirs[entry.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if len(out) >= maxScanFiles {
				return filepath.SkipAll
			}
			if entry.Type().IsRegular() && isShellFile(path, entry) {
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("scanning workspace root %s: %w", root, err)
		}
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

func WatchGlobs() []string {
	extensions := make([]string, len(shellExtensions))
	for i, ext := range shellExtensions {
		extensions[i] = strings.TrimPrefix(ext, ".")
	}
	names := make([]string, 0, len(rcFiles))
	for name := range rcFiles {
		names = append(names, strings.TrimPrefix(name, "."))
	}
	slices.Sort(names)
	return []string{
		"**/*.{" + strings.Join(extensions, ",") + "}",
		"**/.{" + strings.Join(names, ",") + "}",
	}
}

func isShellFile(path string, entry fs.DirEntry) bool {
	if rcFiles[entry.Name()] || slices.Contains(shellExtensions, filepath.Ext(path)) {
		return true
	}
	if filepath.Ext(path) != "" {
		return false
	}
	info, err := entry.Info()
	if err != nil || info.Mode().Perm()&0o111 == 0 {
		return false
	}
	return hasShellShebang(path)
}

func hasShellShebang(path string) bool {
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, shebangProbe)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return false
	}
	line, _, _ := bytes.Cut(head[:n], []byte("\n"))
	_, ok := dialect.FromShebang(string(line))
	return ok
}
