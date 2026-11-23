package gitrepo

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/stubbedev/shell-lsp/internal/regularfile"
)

const (
	optionalPrefix    = ":(optional)"
	prefixVariable    = "%(prefix)/"
	tilde             = "~"
	gitdirCondition   = "gitdir:"
	gitdirFoldCond    = "gitdir/i:"
	onBranchCondition = "onbranch:"
	remoteURLCond     = "hasconfig:remote.*.url:"
	headFile          = "HEAD"
	symbolicRef       = "ref:"
	branchPrefix      = "refs/heads/"
	filesRefStorage   = "files"
	worktreeFile      = "config.worktree"
	maxIncludeDepth   = 10
	maxFormatVersion  = 1
	maxSymrefDepth    = 5
	sha256Format      = "sha256"
	sha1Length        = 40
	sha256Length      = 64
)

var (
	errRelativeInclude   = errors.New("a relative include path must come from a config file")
	errRelativeCondition = errors.New("a relative gitdir condition must come from a config file")
	errURLUnderCondition = errors.New("remote URLs cannot be set in a file an includeIf includes while a hasconfig:remote.*.url condition is in use")
	errReftable          = errors.New("the repository keeps HEAD in a reftable, which shell-lsp does not read")
	errFormatVersion     = fmt.Errorf("git reads repositories only up to format version %d", maxFormatVersion)
)

type expander struct {
	home     string
	prefix   func() (string, error)
	userHome func(string) (string, error)
	realPath func(string) (string, error)
}

func (e expander) interpolate(path string, realHome bool) (string, error) {
	if rest, ok := strings.CutPrefix(path, prefixVariable); ok {
		if e.prefix == nil {
			return "", fmt.Errorf("the path %q starts with %s, git's installation prefix, which is unknown", path, prefixVariable)
		}
		prefix, err := e.prefix()
		if err != nil {
			return "", fmt.Errorf("expanding %q: %w", path, err)
		}
		return prefix + "/" + rest, nil
	}
	rest, ok := strings.CutPrefix(path, tilde)
	if !ok {
		return path, nil
	}
	name, rest, slash := strings.Cut(rest, "/")
	home, err := e.homeOf(name, realHome)
	if err != nil {
		return "", fmt.Errorf("expanding %q: %w", path, err)
	}
	if !slash {
		return home, nil
	}
	return home + "/" + rest, nil
}

func (e expander) homeOf(name string, realHome bool) (string, error) {
	if name != "" {
		if e.userHome == nil {
			return "", fmt.Errorf("the home directory of %q cannot be looked up", name)
		}
		return e.userHome(name)
	}
	if e.home == "" {
		return "", fmt.Errorf("the home directory: %w", errNotExpanded)
	}
	if !realHome {
		return e.home, nil
	}
	real, err := e.realPath(e.home)
	if err != nil {
		return "", fmt.Errorf("resolving the home directory %s: %w", e.home, err)
	}
	return real, nil
}

type pathValue struct {
	path     string
	optional bool
}

func (e expander) pathname(value, base string) (pathValue, error) {
	rest, optional := strings.CutPrefix(value, optionalPrefix)
	if rest == "" {
		return pathValue{optional: optional}, nil
	}
	expanded, err := e.interpolate(rest, false)
	if err != nil {
		return pathValue{}, err
	}
	expanded = filepath.FromSlash(expanded)
	if !filepath.IsAbs(expanded) {
		if base == "" {
			return pathValue{}, fmt.Errorf("%q: %w", value, errRelativeInclude)
		}
		expanded = filepath.Join(base, expanded)
	}
	return pathValue{path: filepath.Clean(expanded), optional: optional}, nil
}

type source struct {
	path     string
	label    string
	settings config
}

func (s source) origin(st setting) string {
	if s.path != "" {
		return fmt.Sprintf("%s:%d", s.path, st.line)
	}
	return s.label
}

type repositoryFormat struct {
	version        int64
	worktreeConfig bool
	refStorage     string
	objectFormat   string
}

func readFormat(path string, settings config) (repositoryFormat, error) {
	var out repositoryFormat
	for _, s := range settings {
		switch s.key {
		case formatVersion:
			version, err := parseInt(s.value)
			if err != nil {
				return repositoryFormat{}, fmt.Errorf("%s:%d: core.repositoryFormatVersion: %w", path, s.line, err)
			}
			out.version = version
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
		case excludesFile, include, conditionalInclude, remoteURL:
		}
	}
	if out.version > maxFormatVersion {
		return repositoryFormat{}, fmt.Errorf("%s: core.repositoryFormatVersion is %d: %w", path, out.version, errFormatVersion)
	}
	return out, nil
}

func (f repositoryFormat) objectIDLength() int {
	if f.version >= 1 && f.objectFormat == sha256Format {
		return sha256Length
	}
	return sha1Length
}

type assignment struct {
	named pathValue
	set   bool
}

type walker struct {
	g          *Ignores
	top        string
	dirs       Dirs
	format     repositoryFormat
	sources    []source
	collecting bool
	urls       []string
	collected  bool
	last       assignment
}

func (w *walker) run() error {
	for _, s := range w.sources {
		if err := w.source(s); err != nil {
			return err
		}
	}
	return nil
}

func (w *walker) source(s source) error {
	if s.path != "" {
		return w.file(s.path, 0, false)
	}
	return w.visit(s, 0, false)
}

func (w *walker) file(path string, depth int, within bool) error {
	if depth > maxIncludeDepth {
		return fmt.Errorf("including %s goes past %d nested includes", path, maxIncludeDepth)
	}
	settings, _, err := w.g.configs.Load(path)
	if err != nil {
		return err
	}
	return w.visit(source{path: path, settings: settings}, depth, within)
}

func (w *walker) visit(s source, depth int, within bool) error {
	base := ""
	if s.path != "" {
		base = filepath.Dir(s.path)
	}
	for _, st := range s.settings {
		origin := s.origin(st)
		switch st.key {
		case excludesFile:
			if w.collecting {
				continue
			}
			named, err := w.g.expand.pathname(st.value, w.top)
			if err != nil {
				return fmt.Errorf("%s: %w", origin, err)
			}
			w.last = assignment{named: named, set: true}
		case remoteURL:
			if !w.collecting {
				continue
			}
			if within {
				return fmt.Errorf("%s: %w", origin, errURLUnderCondition)
			}
			w.urls = append(w.urls, st.value)
		case include:
			if err := w.include(st.value, base, origin, depth, within); err != nil {
				return err
			}
		case conditionalInclude:
			holds, err := w.holds(st.condition, s.path)
			if err != nil {
				return fmt.Errorf("%s: includeIf %q: %w", origin, st.condition, err)
			}
			if !holds {
				continue
			}
			if err := w.include(st.value, base, origin, depth, true); err != nil {
				return err
			}
		case worktreeConfig, refStorage, formatVersion, objectFormat:
		}
	}
	return nil
}

func (w *walker) include(value, base, origin string, depth int, within bool) error {
	if value == "" {
		return fmt.Errorf("%s: %w", origin, errEmptyInclude)
	}
	named, err := w.g.expand.pathname(value, base)
	if err != nil {
		return fmt.Errorf("%s: %w", origin, err)
	}
	return w.file(named.path, depth+1, within)
}

func (w *walker) holds(condition, path string) (bool, error) {
	if pattern, ok := strings.CutPrefix(condition, gitdirCondition); ok {
		return w.gitdir(pattern, path, false)
	}
	if pattern, ok := strings.CutPrefix(condition, gitdirFoldCond); ok {
		return w.gitdir(pattern, path, true)
	}
	if pattern, ok := strings.CutPrefix(condition, onBranchCondition); ok {
		return w.onBranch(pattern)
	}
	if pattern, ok := strings.CutPrefix(condition, remoteURLCond); ok {
		return w.remoteURL(pattern)
	}
	return false, nil
}

func (w *walker) gitdir(pattern, path string, fold bool) (bool, error) {
	pattern, prefix, err := w.gitdirPattern(pattern, path)
	if err != nil {
		return false, err
	}
	real, err := w.g.expand.realPath(w.dirs.Git)
	if err != nil {
		return false, fmt.Errorf("resolving the git directory %s: %w", w.dirs.Git, err)
	}
	for _, text := range []string{real, w.dirs.Git} {
		if prefixMatch(pattern, filepath.ToSlash(text), prefix, fold) {
			return true, nil
		}
	}
	return false, nil
}

func (w *walker) gitdirPattern(pattern, path string) (string, int, error) {
	expanded, err := w.g.expand.interpolate(pattern, true)
	switch {
	case errors.Is(err, errNotExpanded):
	case err != nil:
		return "", 0, err
	default:
		pattern = filepath.ToSlash(expanded)
	}
	prefix := 0
	if rest, ok := strings.CutPrefix(pattern, "./"); ok {
		if path == "" {
			return "", 0, errRelativeCondition
		}
		real, err := w.g.expand.realPath(path)
		if err != nil {
			return "", 0, fmt.Errorf("resolving the config file %s: %w", path, err)
		}
		dir := filepath.ToSlash(filepath.Dir(real))
		pattern, prefix = dir+"/"+rest, len(dir)+1
	} else if !filepath.IsAbs(filepath.FromSlash(pattern)) {
		pattern = "**/" + pattern
	}
	return withTrailingStars(pattern), prefix, nil
}

func withTrailingStars(pattern string) string {
	if strings.HasSuffix(pattern, "/") {
		return pattern + "**"
	}
	return pattern
}

func prefixMatch(pattern, text string, prefix int, fold bool) bool {
	if prefix > 0 {
		if len(text) < prefix {
			return false
		}
		if fold && !strings.EqualFold(pattern[:prefix], text[:prefix]) || !fold && pattern[:prefix] != text[:prefix] {
			return false
		}
	}
	return wildmatch(pattern[prefix:], text[prefix:], fold)
}

func (w *walker) onBranch(pattern string) (bool, error) {
	if w.format.refStorage != "" && w.format.refStorage != filesRefStorage {
		return false, errReftable
	}
	name, resolved, err := w.resolveRef(headFile)
	if err != nil || !resolved {
		return false, err
	}
	branch, ok := strings.CutPrefix(name, branchPrefix)
	if !ok {
		return false, nil
	}
	return wildmatch(withTrailingStars(pattern), branch, false), nil
}

func (w *walker) resolveRef(name string) (string, bool, error) {
	for range maxSymrefDepth {
		text, found, err := w.g.texts.Load(w.refPath(name))
		switch {
		case refAbsent(err):
			return name, true, nil
		case err != nil:
			return "", false, err
		case !found:
			return name, true, nil
		}
		target, symbolic := strings.CutPrefix(text, symbolicRef)
		if !symbolic {
			return name, w.isObjectID(text), nil
		}
		name = strings.TrimRight(strings.TrimLeft(target, cSpace), cSpace)
		if !validRefName(name) {
			return "", false, nil
		}
	}
	return "", false, nil
}

func refAbsent(err error) bool {
	return errors.Is(err, syscall.ENOTDIR) || errors.Is(err, syscall.EISDIR) || errors.Is(err, regularfile.ErrNotRegular)
}

func (w *walker) refPath(name string) string {
	dir := w.dirs.Common
	if !strings.Contains(name, "/") || hasAnyPrefix(name, perWorktreeRefs) {
		dir = w.dirs.Git
	}
	return filepath.Join(dir, filepath.FromSlash(name))
}

var perWorktreeRefs = []string{"refs/worktree/", "refs/bisect/", "refs/rewritten/"}

func hasAnyPrefix(text string, prefixes []string) bool {
	return slices.ContainsFunc(prefixes, func(prefix string) bool { return strings.HasPrefix(text, prefix) })
}

func (w *walker) isObjectID(text string) bool {
	n := w.format.objectIDLength()
	if len(text) < n || len(text) > n && !isCSpace(text[n]) {
		return false
	}
	for _, c := range []byte(text[:n]) {
		if digitValue(c) >= 16 {
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

func (w *walker) remoteURL(pattern string) (bool, error) {
	if w.collecting {
		return true, nil
	}
	if !w.collected {
		collector := walker{g: w.g, top: w.top, dirs: w.dirs, format: w.format, sources: w.sources, collecting: true}
		if err := collector.run(); err != nil {
			return false, err
		}
		w.urls, w.collected = collector.urls, true
	}
	for _, url := range w.urls {
		if wildmatch(pattern, url, false) {
			return true, nil
		}
	}
	return false, nil
}
