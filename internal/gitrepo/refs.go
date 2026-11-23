package gitrepo

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/stubbedev/shell-lsp/internal/regularfile"
)

const (
	packedRefsFile     = "packed-refs"
	otherWorktreeRefs  = "worktrees/"
	mainWorktreeRefs   = "main-worktree/"
	worktreesDir       = "worktrees"
	packedRefSeparator = " "
)

var (
	errReftable     = errors.New("the repository keeps its refs in a reftable, which shell-lsp does not read")
	perWorktreeRefs = []string{"refs/worktree/", "refs/bisect/", "refs/rewritten/"}
)

type Ref struct {
	Name string
	id   string
}

func (r Repository) UsesReftable() bool { return r.format.usesReftable() }

func (r Repository) ResolveRef(read ReadText, name string) (Ref, bool, error) {
	if r.UsesReftable() {
		return Ref{}, false, errReftable
	}
	for range maxSymrefDepth {
		text, stored, err := r.looseRef(read, name)
		if err != nil || !stored {
			return Ref{Name: name}, err == nil, err
		}
		target, symbolic := strings.CutPrefix(text, symbolicRef)
		if !symbolic {
			n := r.format.objectIDLength()
			if !hexPrefix(text, n) || len(text) > n && !isCSpace(text[n]) {
				return Ref{}, false, nil
			}
			return Ref{Name: name, id: text[:n]}, true, nil
		}
		name = strings.Trim(target, cSpace)
		if !validRefName(name) {
			return Ref{}, false, nil
		}
	}
	return Ref{}, false, nil
}

func (r Repository) looseRef(read ReadText, name string) (string, bool, error) {
	path := r.refPath(name)
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOTDIR):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("reading the ref %s: %w", path, err)
	case info.IsDir():
		return "", false, nil
	}
	text, found, err := read(path)
	switch {
	case errors.Is(err, regularfile.ErrNotRegular):
		return "", false, nil
	case err != nil:
		return "", false, err
	}
	return text, found, nil
}

func (r Repository) ObjectID(readPacked ReadText, ref Ref) (string, bool, error) {
	if ref.id != "" {
		return ref.id, true, nil
	}
	text, found, err := readPacked(filepath.Join(r.Dirs.Common, packedRefsFile))
	if err != nil || !found {
		return "", false, err
	}
	for line := range strings.Lines(text) {
		id, name, ok := strings.Cut(strings.TrimRight(line, lineEnds), packedRefSeparator)
		if ok && name == ref.Name && len(id) == r.format.objectIDLength() && hexPrefix(id, len(id)) {
			return id, true, nil
		}
	}
	return "", false, nil
}

func (r Repository) refPath(name string) string {
	if rest, ok := strings.CutPrefix(name, otherWorktreeRefs); ok {
		id, ref, named := strings.Cut(rest, "/")
		if !named || currentWorktreeRef(ref) {
			return filepath.Join(r.Dirs.Common, worktreesDir, id, filepath.FromSlash(ref))
		}
	}
	if ref, ok := strings.CutPrefix(name, mainWorktreeRefs); ok && currentWorktreeRef(ref) {
		return filepath.Join(r.Dirs.Common, filepath.FromSlash(ref))
	}
	if currentWorktreeRef(name) {
		return filepath.Join(r.Dirs.Git, filepath.FromSlash(name))
	}
	return filepath.Join(r.Dirs.Common, filepath.FromSlash(name))
}

func currentWorktreeRef(name string) bool {
	return rootRefSyntax(name) || slices.ContainsFunc(perWorktreeRefs, func(prefix string) bool { return strings.HasPrefix(name, prefix) })
}

func rootRefSyntax(name string) bool {
	for _, c := range []byte(name) {
		if (c < 'A' || c > 'Z') && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

func validRefName(name string) bool {
	if name == "" || name == "@" || strings.HasSuffix(name, "/") || strings.HasSuffix(name, ".") || strings.Contains(name, "..") || strings.Contains(name, "@{") {
		return false
	}
	for component := range strings.SplitSeq(name, "/") {
		if component == "" || strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".lock") {
			return false
		}
	}
	for _, c := range []byte(name) {
		if c < ' ' || c == 0x7f || strings.IndexByte(" ~^:?*[\\", c) >= 0 {
			return false
		}
	}
	return true
}
