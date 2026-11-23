package gitrepo

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

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
		ref, stored, err := r.looseRef(read, name)
		if err != nil || !stored {
			return Ref{Name: name}, err == nil, err
		}
		if !ref.symbolic {
			n := r.format.objectIDLength()
			if !hexPrefix(ref.text, n) || len(ref.text) > n && !isGitSpace(ref.text[n]) {
				return Ref{}, false, nil
			}
			return Ref{Name: name, id: ref.text[:n]}, true, nil
		}
		if !validRefName(ref.text) {
			return Ref{}, false, nil
		}
		name = ref.text
	}
	return Ref{}, false, nil
}

type looseRef struct {
	text     string
	symbolic bool
}

func (r Repository) looseRef(read ReadText, name string) (looseRef, bool, error) {
	path := r.refPath(name)
	info, err := os.Lstat(path)
	switch {
	case missing(err):
		return looseRef{}, false, nil
	case err != nil:
		return looseRef{}, false, fmt.Errorf("reading the ref %s: %w", path, err)
	case info.Mode()&fs.ModeSymlink != 0:
		return linkedRef(read, path)
	}
	return refFile(read, path, info)
}

func linkTarget(path string) (string, error) {
	target, err := os.Readlink(path)
	return filepath.ToSlash(target), err
}

func linkedRef(read ReadText, path string) (looseRef, bool, error) {
	target, err := linkTarget(path)
	if err != nil {
		return looseRef{}, false, fmt.Errorf("reading the ref %s: %w", path, err)
	}
	if strings.HasPrefix(target, refsPrefix) && validRefName(target) {
		return looseRef{text: target, symbolic: true}, true, nil
	}
	info, err := os.Stat(path)
	switch {
	case missing(err):
		return looseRef{}, false, nil
	case err != nil:
		return looseRef{}, false, fmt.Errorf("reading the ref %s: %w", path, err)
	}
	return refFile(read, path, info)
}

func refFile(read ReadText, path string, info fs.FileInfo) (looseRef, bool, error) {
	if info.IsDir() {
		return looseRef{}, false, nil
	}
	text, found, err := read(path)
	switch {
	case errors.Is(err, regularfile.ErrNotRegular):
		return looseRef{}, false, nil
	case err != nil || !found:
		return looseRef{}, false, err
	}
	if target, symbolic := strings.CutPrefix(text, symbolicRef); symbolic {
		return looseRef{text: strings.Trim(target, gitSpace), symbolic: true}, true, nil
	}
	return looseRef{text: text}, true, nil
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
