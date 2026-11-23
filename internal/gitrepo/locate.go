package gitrepo

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/stubbedev/shell-lsp/internal/platform"
	"github.com/stubbedev/shell-lsp/internal/regularfile"
)

const (
	gitDirVariable    = "GIT_DIR"
	workTreeVariable  = "GIT_WORK_TREE"
	commonDirVariable = "GIT_COMMON_DIR"
	ceilingVariable   = "GIT_CEILING_DIRECTORIES"
	acrossFSVariable  = "GIT_DISCOVERY_ACROSS_FILESYSTEM"
	objectsDir        = "objects"
	refsDir           = "refs"
	refsPrefix        = "refs/"
	maxHeadPeek       = 255
)

var processVariables = []string{gitDirVariable, workTreeVariable, commonDirVariable, ceilingVariable, acrossFSVariable}

type Repository struct {
	Top    string
	Prefix string
	Dirs   Dirs
	format repositoryFormat
}

type probe struct {
	track  func(path string)
	device func(path string) (uint64, error)
}

func Discover(dir string) (Repository, bool, error) {
	return Locator{}.find(dir, probe{track: func(string) {}, device: platform.DeviceOf})
}

type Locator struct {
	search  search
	process *processLocation
	err     error
}

type search struct {
	ceilings          []string
	acrossFilesystems bool
}

type processLocation struct {
	cwd       place
	gitDir    string
	workTree  string
	commonDir string
}

func (p Process) Discovery() Locator {
	across, err := parseBool(p.Variables[acrossFSVariable])
	if err != nil {
		return Locator{err: fmt.Errorf("%s: %w", acrossFSVariable, err)}
	}
	return Locator{search: search{ceilings: ceilings(p.Variables[ceilingVariable], filepath.EvalSymlinks), acrossFilesystems: across}}
}

func ceilings(list string, realPath func(string) (string, error)) []string {
	var out []string
	verbatim := false
	for entry := range strings.SplitSeq(list, string(filepath.ListSeparator)) {
		switch {
		case entry == "":
			verbatim = true
		case !filepath.IsAbs(entry):
		case verbatim:
			out = append(out, filepath.Clean(entry))
		default:
			if real, ok := resolveCeiling(entry, realPath); ok {
				out = append(out, filepath.Clean(entry), real)
			}
		}
	}
	return out
}

func resolveCeiling(path string, realPath func(string) (string, error)) (string, bool) {
	real, err := realPath(path)
	if err == nil {
		return real, true
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", false
	}
	parent, err := realPath(filepath.Dir(path))
	if err != nil {
		return "", false
	}
	return filepath.Join(parent, filepath.Base(path)), true
}

func (p Process) Locate(cwd string) Locator {
	locator := p.Discovery()
	if locator.err != nil {
		return locator
	}
	var location processLocation
	set := false
	var at place
	for _, v := range []struct {
		name string
		path *string
	}{{gitDirVariable, &location.gitDir}, {workTreeVariable, &location.workTree}, {commonDirVariable, &location.commonDir}} {
		value, ok := p.Variables[v.name]
		if !ok {
			continue
		}
		if value == "" {
			return Locator{err: fmt.Errorf("%s is set to the empty string, which git does not accept as a path", v.name)}
		}
		if !set {
			var err error
			if at, err = placeOf(cwd); err != nil {
				return Locator{err: err}
			}
		}
		set = true
		*v.path = filepath.Clean(value)
		if !filepath.IsAbs(value) {
			*v.path = filepath.Join(at.real, value)
		}
	}
	if !set {
		return locator
	}
	location.cwd = at
	locator.process = &location
	return locator
}

func (l Locator) find(dir string, p probe) (Repository, bool, error) {
	if l.err != nil {
		return Repository{}, false, l.err
	}
	at, err := placeOf(dir)
	if err != nil {
		return Repository{}, false, err
	}
	location := processLocation{cwd: at}
	if l.process != nil {
		location = *l.process
	}
	repo, worked, err := l.search.open(location, p)
	if err != nil || !worked {
		return Repository{}, false, err
	}
	prefix, within := platform.RelativeWithin(repo.Top, at.real)
	if !within {
		return Repository{}, false, nil
	}
	repo.Top, repo.Prefix = at.alias(prefix, repo.Top), prefix
	return repo, true, nil
}

type place struct {
	written string
	real    string
}

func placeOf(dir string) (place, error) {
	real, err := realDir(dir)
	if err != nil {
		return place{}, fmt.Errorf("resolving %s while looking for a git repository: %w", dir, err)
	}
	return place{written: dir, real: real}, nil
}

func realDir(dir string) (string, error) {
	real, err := filepath.EvalSymlinks(dir)
	if err == nil || !missing(err) {
		return real, err
	}
	parent := filepath.Dir(dir)
	if parent == dir {
		return "", err
	}
	real, err = realDir(parent)
	return filepath.Join(real, filepath.Base(dir)), err
}

func missing(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

func (at place) alias(prefix, top string) string {
	if prefix == "." {
		return at.written
	}
	written := at.written
	for range strings.SplitSeq(prefix, string(filepath.Separator)) {
		parent := filepath.Dir(written)
		if parent == written {
			return top
		}
		written = parent
	}
	if real, err := filepath.EvalSymlinks(written); err != nil || real != top {
		return top
	}
	return written
}

func (s search) open(location processLocation, p probe) (Repository, bool, error) {
	git, implicitTop, found, err := s.gitDirectory(location, p)
	if err != nil {
		return Repository{}, false, err
	}
	if !found {
		if location.workTree != "" || location.commonDir != "" {
			return Repository{}, false, fmt.Errorf("%s or %s is set, but %s is not in a git repository", workTreeVariable, commonDirVariable, location.cwd.written)
		}
		return Repository{}, false, nil
	}
	format, err := readRepositoryFormat(git, p)
	if err != nil {
		return Repository{}, false, err
	}
	top, worked, err := format.workTreeOf(git.Git, location.workTree, implicitTop, p)
	if err != nil || !worked {
		return Repository{}, false, err
	}
	return Repository{Top: top, Dirs: git.Dirs, format: format}, true, nil
}

type gitDirectory struct {
	Dirs
	shared bool
}

func (s search) gitDirectory(location processLocation, p probe) (gitDirectory, string, bool, error) {
	if location.gitDir == "" {
		return s.discover(location.cwd, location.commonDir, p)
	}
	cwd := location.cwd.real
	path := location.gitDir
	p.track(path)
	info, err := os.Stat(path)
	if err != nil {
		return gitDirectory{}, "", false, fmt.Errorf("%s=%s is not a git repository: %w", gitDirVariable, path, err)
	}
	switch {
	case info.Mode().IsRegular():
		git, err := readGitFile(path, location.commonDir, p)
		return git, cwd, err == nil, err
	case !info.IsDir():
		return gitDirectory{}, "", false, fmt.Errorf("%s=%s is a %s, not a git repository", gitDirVariable, path, info.Mode().Type())
	}
	git, valid, err := openGitDirectory(path, location.commonDir, p)
	if err != nil {
		return gitDirectory{}, "", false, err
	}
	if !valid {
		return gitDirectory{}, "", false, fmt.Errorf("%s=%s is not a git repository: it lacks a valid HEAD, objects or refs", gitDirVariable, path)
	}
	return git, cwd, true, nil
}

func (s search) discover(at place, commonDir string, p probe) (gitDirectory, string, bool, error) {
	ceiling := s.ceiling(at.real)
	var start filesystem
	if _, err := s.leaves(&start, at.real, p); err != nil {
		return gitDirectory{}, "", false, err
	}
	for look, current := at.written, at.real; ; look = current {
		git, found, err := gitDirectoryAt(look, commonDir, p)
		if err != nil || found {
			return git, current, found, err
		}
		git, bare, err := openGitDirectory(look, commonDir, p)
		if err != nil || bare {
			return git, "", bare, err
		}
		parent := filepath.Dir(current)
		if parent == current || ceiling != "" && sameDir(parent, ceiling) {
			return gitDirectory{}, "", false, nil
		}
		crossed, err := s.leaves(&start, parent, p)
		if err != nil || crossed {
			return gitDirectory{}, "", false, err
		}
		current = parent
	}
}

type filesystem struct {
	device uint64
	known  bool
}

func (s search) leaves(start *filesystem, dir string, p probe) (bool, error) {
	if s.acrossFilesystems {
		return false, nil
	}
	device, err := p.device(dir)
	switch {
	case missing(err):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("finding the filesystem of %s while looking for a git repository: %w", dir, err)
	case !start.known:
		*start = filesystem{device: device, known: true}
		return false, nil
	}
	return device != start.device, nil
}

func (s search) ceiling(dir string) string {
	best := ""
	for _, c := range s.ceilings {
		if len(c) > len(best) && platform.Native.Within(dir, c) && !sameDir(dir, c) {
			best = c
		}
	}
	return best
}

func sameDir(a, b string) bool {
	return platform.Native.Fold(filepath.Clean(a)) == platform.Native.Fold(filepath.Clean(b))
}

func gitDirectoryAt(dir, commonDir string, p probe) (gitDirectory, bool, error) {
	marker := filepath.Join(dir, MarkerName)
	p.track(marker)
	info, err := os.Stat(marker)
	switch {
	case missing(err):
		return gitDirectory{}, false, nil
	case err != nil:
		return gitDirectory{}, false, fmt.Errorf("looking for a git repository at %s: %w", marker, err)
	case info.IsDir():
		return openGitDirectory(marker, commonDir, p)
	case info.Mode().IsRegular():
		git, err := readGitFile(marker, commonDir, p)
		return git, err == nil, err
	}
	return gitDirectory{}, false, fmt.Errorf("%s is a %s, neither a git directory nor a gitdir: file", marker, info.Mode().Type())
}

func openGitDirectory(path, commonDir string, p probe) (gitDirectory, bool, error) {
	if !validHead(filepath.Join(path, headFile), p) {
		return gitDirectory{}, false, nil
	}
	git, err := commonDirOf(path, commonDir, p)
	if err != nil {
		return gitDirectory{}, false, err
	}
	for _, name := range []string{objectsDir, refsDir} {
		sub := filepath.Join(git.Common, name)
		p.track(sub)
		if !searchable(sub) {
			return gitDirectory{}, false, nil
		}
	}
	return git, true, nil
}

func validHead(path string, p probe) bool {
	p.track(path)
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		return err == nil && strings.HasPrefix(target, refsPrefix)
	}
	f, _, err := regularfile.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	peek := make([]byte, maxHeadPeek)
	n, err := io.ReadFull(f, peek)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return false
	}
	text, _, _ := strings.Cut(string(peek[:n]), "\x00")
	if target, symbolic := strings.CutPrefix(text, symbolicRef); symbolic {
		return strings.HasPrefix(strings.TrimLeft(target, gitSpace), refsPrefix)
	}
	return hexPrefix(text, sha1Length)
}

func hexPrefix(text string, n int) bool {
	if len(text) < n {
		return false
	}
	for _, c := range []byte(text[:n]) {
		if digitValue(c) >= 16 {
			return false
		}
	}
	return true
}
