package gitrepo

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
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

type assignment struct {
	named pathValue
	set   bool
}

type walker struct {
	g          *Ignores
	repo       Repository
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
			named, err := w.g.expand.pathname(st.value, w.repo.Top)
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
		case worktreeConfig, refStorage, formatVersion, objectFormat, workTree, bare:
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
	real, err := w.g.expand.realPath(w.repo.Dirs.Git)
	if err != nil {
		return false, fmt.Errorf("resolving the git directory %s: %w", w.repo.Dirs.Git, err)
	}
	for _, text := range []string{real, w.repo.Dirs.Git} {
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
	head, resolved, err := w.repo.ResolveRef(w.g.texts.Load, headFile)
	if err != nil || !resolved {
		return false, err
	}
	branch, ok := strings.CutPrefix(head.Name, branchPrefix)
	if !ok {
		return false, nil
	}
	return wildmatch(withTrailingStars(pattern), branch, false), nil
}

func (w *walker) remoteURL(pattern string) (bool, error) {
	if w.collecting {
		return true, nil
	}
	if !w.collected {
		collector := walker{g: w.g, repo: w.repo, sources: w.sources, collecting: true}
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
