package cli

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/editorconfig"
	"github.com/stubbedev/shell-lsp/internal/filelint"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/parallel"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/platform"
	"github.com/stubbedev/shell-lsp/internal/project"
	"github.com/stubbedev/shell-lsp/internal/regularfile"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

const (
	stdinName    = "-"
	parseFailure = lint.Code(1072)
)

func runCheck(env Env, args []string) int {
	opts, err := parseCheckArgs(args)
	if err != nil {
		fmt.Fprintf(env.Stderr, "%s check: %v\n", programName, err)
		if checkStatusOf(err) == checkStatusSyntax {
			fmt.Fprintf(env.Stderr, "\n%s", checkUsage)
		}
		return checkStatusOf(err)
	}
	switch {
	case opts.help:
		fmt.Fprint(env.Stdout, checkUsage)
		return statusOK
	case opts.version:
		fmt.Fprintln(env.Stdout, versionPrefix+env.Version)
		return statusOK
	case opts.listOptional:
		for _, name := range lint.OptionalChecks() {
			fmt.Fprintln(env.Stdout, name)
		}
		return statusOK
	case len(opts.operands) == 0:
		fmt.Fprintf(env.Stderr, "%s check: no files specified\n\n%s", programName, checkUsage)
		return checkStatusSyntax
	}
	warnUnknownOptional(env.Stderr, opts.enable, "--enable")
	c := newChecker(env, opts)
	reports, failures, configErr := c.run()
	if configErr != nil {
		fmt.Fprintf(env.Stderr, "%s check: %v\n", programName, configErr)
		return checkStatusOptions
	}
	for _, failure := range failures {
		fmt.Fprintln(env.Stderr, failure)
	}
	var out bytes.Buffer
	opts.format.write(&out, reports, reportOptions{color: c.color(), wikiLinks: opts.wikiLinks, version: env.Version})
	if _, err := env.Stdout.Write(out.Bytes()); err != nil {
		fmt.Fprintf(env.Stderr, "%s check: writing the report: %v\n", programName, err)
		return checkStatusFiles
	}
	found := slices.ContainsFunc(reports, func(r fileReport) bool { return len(r.findings) > 0 })
	if found && out.Len() == 0 && opts.format.emptyNote != "" {
		fmt.Fprintln(env.Stderr, opts.format.emptyNote)
	}
	switch {
	case len(failures) > 0:
		return checkStatusFiles
	case found:
		return checkStatusFindings
	}
	return statusOK
}

func warnUnknownOptional(stderr io.Writer, names []string, from string) {
	known := lint.OptionalChecks()
	for _, name := range names {
		if name != allCodes && !slices.Contains(known, name) {
			fmt.Fprintf(stderr, "%s check: %s names %q, which is not an optional check of shell-lsp (see --list-optional); it is ignored\n", programName, from, name)
		}
	}
}

type checker struct {
	env      Env
	opts     checkOptions
	editor   *editorconfig.Resolver
	rcs      *rcFinder
	ws       *workspace.Workspace
	resolved sync.Map
	warned   sync.Map
	stdin    func() ([]byte, error)
}

type resolution struct {
	r   project.Resolution
	err error
}

type target struct {
	name string
	path string
}

func newChecker(env Env, opts checkOptions) *checker {
	if opts.rc.mode == rcExplicit && !filepath.IsAbs(opts.rc.path) {
		opts.rc.path = filepath.Join(env.Dir, opts.rc.path)
	}
	c := &checker{
		env:    env,
		opts:   opts,
		editor: editorconfig.NewResolver(regularfile.ReadConfig, time.Now, time.Hour),
		rcs:    newRCFinder(opts.rc, regularfile.ReadConfig, env.Home, env.ConfigHome),
		stdin:  sync.OnceValues(func() ([]byte, error) { return io.ReadAll(env.Stdin) }),
	}
	c.ws = workspace.New(func(string) (*workspace.File, bool) { return nil, false }, c.dialectOf)
	return c
}

func (c *checker) color() bool {
	switch c.opts.color {
	case colorAlways:
		return true
	case colorNever:
		return false
	}
	return c.env.Terminal
}

type fileSettings struct {
	rc       rcFile
	forced   *dialect.Dialect
	exclude  codeSet
	optional []string
	extended bool
	roots    []string
}

func (c *checker) settings(dir string) (fileSettings, error) {
	rc, err := c.rcs.forDir(dir)
	if err != nil {
		return fileSettings{}, err
	}
	if rc.path != "" {
		if _, warned := c.warned.LoadOrStore(rc.path, true); !warned {
			warnUnknownOptional(c.env.Stderr, rc.enabled, rc.path)
		}
	}
	s := fileSettings{
		rc:       rc,
		forced:   cmp.Or(c.opts.shell, rc.shell),
		exclude:  c.opts.exclude.union(rc.disabled),
		optional: slices.Concat(c.opts.enable, rc.enabled),
		extended: *cmp.Or(c.opts.extended, rc.extended, new(true)),
	}
	for _, p := range slices.Concat(c.opts.sourcePaths, rc.sourcePaths) {
		if p == scriptDir {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(c.env.Dir, p)
		}
		s.roots = append(s.roots, p)
	}
	s.roots = append(s.roots, c.env.Dir)
	return s, nil
}

func (c *checker) dialectOf(path string, in dialect.Input) dialect.Dialect {
	s, err := c.settings(filepath.Dir(path))
	if err != nil {
		c.resolved.Store(path, resolution{err: &configError{err}})
		return dialect.Detect(nil, in)
	}
	r, err := project.Resolve(c.editor, path, s.forced, in)
	if err != nil {
		c.resolved.Store(path, resolution{err: &configError{err}})
		return r.Dialect
	}
	c.resolved.Store(path, resolution{r: r})
	return r.Dialect
}

func (c *checker) navigationEnv(roots []string) navigation.Env {
	return navigation.Env{Loader: staticLoader{c.ws}, Roots: roots, Home: c.env.Home, PathDirs: c.env.PathDirs}
}

type staticLoader struct {
	ws *workspace.Workspace
}

func (l staticLoader) Load(path string) (*workspace.File, error) { return l.ws.Load(path) }

func (staticLoader) Generation() uint64 { return 0 }

type linted struct {
	report  fileReport
	file    *workspace.File
	env     navigation.Env
	failure string
	config  error
}

func (c *checker) run() ([]fileReport, []string, error) {
	targets, failures := c.targets()
	results := c.lintAll(targets)
	if c.opts.checkSourced {
		results = append(results, c.lintAll(c.sourcedTargets(targets, results))...)
	}
	var reports []fileReport
	for _, r := range results {
		switch {
		case r.config != nil:
			return nil, nil, r.config
		case r.failure != "":
			failures = append(failures, r.failure)
		default:
			reports = append(reports, r.report)
		}
	}
	slices.SortFunc(reports, func(a, b fileReport) int { return cmp.Compare(a.name, b.name) })
	slices.Sort(failures)
	return reports, failures, nil
}

func (c *checker) targets() ([]target, []string) {
	var out []target
	var failures []string
	var dirs []string
	for _, operand := range c.opts.operands {
		if operand == stdinName {
			out = append(out, target{name: stdinName})
			continue
		}
		info, err := os.Stat(c.abs(operand))
		switch {
		case err != nil:
			failures = append(failures, fmt.Sprintf("%s: %v", operand, err))
		case info.IsDir():
			dirs = append(dirs, operand)
		default:
			out = append(out, target{name: operand, path: c.abs(operand)})
		}
	}
	for _, dir := range dirs {
		walker := workspace.New(func(string) (*workspace.File, bool) { return nil, false }, workspace.DetectDialect)
		walker.SetRoots([]string{c.abs(dir)})
		paths, err := walker.AllShellFiles(context.Background())
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", dir, err))
			continue
		}
		for _, path := range paths {
			rel, err := filepath.Rel(c.abs(dir), path)
			if err != nil {
				failures = append(failures, fmt.Sprintf("%s: %v", path, err))
				continue
			}
			out = append(out, target{name: filepath.Join(dir, rel), path: path})
		}
	}
	seen := map[string]bool{}
	return slices.DeleteFunc(out, func(t target) bool {
		key := platform.Native.Fold(cmp.Or(t.path, t.name))
		if seen[key] {
			return true
		}
		seen[key] = true
		return false
	}), failures
}

func (c *checker) abs(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(c.env.Dir, path)
}

func (c *checker) display(path string) string {
	if rel, ok := platform.RelativeWithin(c.env.Dir, path); ok {
		return rel
	}
	return path
}

func (c *checker) lintAll(targets []target) []linted {
	out := make([]linted, len(targets))
	parallel.For(runtime.GOMAXPROCS(0), len(targets), func(i int) {
		out[i] = c.lint(targets[i])
	})
	return out
}

func (c *checker) lint(t target) linted {
	file, s, err := c.load(t)
	if err != nil {
		if cfg, ok := errors.AsType[*configError](err); ok {
			return linted{config: cfg.err}
		}
		return linted{failure: fmt.Sprintf("%s: %v", t.name, err)}
	}
	env := c.navigationEnv(s.roots)
	findings := filelint.Check(file, env, filelint.Options{
		Optional:           s.optional,
		DialectSource:      s.source,
		NoExtendedAnalysis: !s.extended,
	})
	return linted{
		report: fileReport{name: t.name, doc: file.Doc, findings: c.filter(s.exclude, append(syntaxFindings(file.Tree, s.source), findings...))},
		file:   file,
		env:    env,
	}
}

type configError struct {
	err error
}

func (e *configError) Error() string { return e.err.Error() }

type loadedSettings struct {
	fileSettings
	source lint.DialectSource
}

func (c *checker) load(t target) (*workspace.File, loadedSettings, error) {
	dir := c.env.Dir
	if t.path != "" {
		dir = filepath.Dir(t.path)
	}
	s, err := c.settings(dir)
	if err != nil {
		return nil, loadedSettings{}, &configError{err}
	}
	file, r, err := c.parse(t, s)
	if err != nil {
		return nil, loadedSettings{}, err
	}
	loaded := loadedSettings{fileSettings: s, source: lint.DialectDetected}
	switch {
	case s.forced != nil:
		loaded.source = lint.DialectForced
	case !r.Named:
		loaded.source = lint.DialectGuessed
	}
	return file, loaded, nil
}

func (c *checker) parse(t target, s fileSettings) (*workspace.File, project.Resolution, error) {
	if t.path == "" {
		raw, err := c.stdin()
		if err != nil {
			return nil, project.Resolution{}, fmt.Errorf("reading standard input: %w", err)
		}
		doc := docstore.NewDocument("", "", 0, string(raw))
		in := dialect.Input{Text: doc.Text()}
		r, err := project.Resolve(c.editor, "", s.forced, in)
		if err != nil {
			return nil, project.Resolution{}, &configError{err}
		}
		return &workspace.File{Doc: doc, Tree: parser.ParseEdit(nil, doc.Text(), r.Dialect, parser.Variant(r.Dialect, in))}, r, nil
	}
	file, err := c.ws.Load(t.path)
	if err != nil {
		return nil, project.Resolution{}, err
	}
	stored, ok := c.resolved.Load(t.path)
	if !ok {
		return nil, project.Resolution{}, fmt.Errorf("resolving the dialect of %s: the workspace parsed it without asking", t.path)
	}
	res, _ := stored.(resolution)
	if res.err != nil {
		return nil, project.Resolution{}, res.err
	}
	return file, res.r, nil
}

func syntaxFindings(tree *parser.Tree, source lint.DialectSource) []lint.Finding {
	if lint.Foreign(tree, source) {
		return nil
	}
	out := make([]lint.Finding, len(tree.Errors))
	for i, e := range tree.Errors {
		out[i] = lint.Finding{Code: parseFailure, Severity: lint.SeverityError, Span: e.Span, Message: e.Message}
	}
	return out
}

func (c *checker) filter(exclude codeSet, findings []lint.Finding) []lint.Finding {
	kept := slices.DeleteFunc(findings, func(f lint.Finding) bool {
		return f.Severity > c.opts.severity || exclude.has(f.Code) || (c.opts.include != nil && !c.opts.include.has(f.Code))
	})
	slices.SortStableFunc(kept, func(a, b lint.Finding) int {
		return cmp.Or(cmp.Compare(a.Span.Start, b.Span.Start), cmp.Compare(a.Code, b.Code), cmp.Compare(a.Span.End, b.Span.End))
	})
	return kept
}

func (c *checker) sourcedTargets(primary []target, results []linted) []target {
	seen := map[string]bool{}
	for _, t := range primary {
		seen[t.path] = true
	}
	var out []target
	for _, r := range results {
		if r.file == nil {
			continue
		}
		for _, f := range navigation.Chain(r.file, r.env) {
			if !seen[f.Path] {
				seen[f.Path] = true
				out = append(out, target{name: c.display(f.Path), path: f.Path})
			}
		}
	}
	return out
}
