package cli

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/editorconfig"
	"github.com/stubbedev/shell-lsp/internal/filelint"
	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/hostnav"
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
		writeOptionalChecks(env.Stdout)
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

func writeOptionalChecks(out io.Writer) {
	for _, check := range lint.OptionalChecks() {
		fmt.Fprintln(out, check.Name)
		if check.Summary != "" {
			fmt.Fprintln(out, "    "+check.Summary)
		}
		for _, code := range check.Codes {
			info, _ := lint.Describe(code)
			fmt.Fprintf(out, "    %s  %s\n", code, info.Summary)
		}
	}
}

func warnUnknownOptional(stderr io.Writer, names []string, from string) {
	for _, name := range names {
		if !lint.EnablesOptional(name) {
			fmt.Fprintf(stderr, "%s check: %s names %q, which is not an optional check of shell-lsp (see --list-optional); it is ignored\n", programName, from, name)
		}
	}
}

type checker struct {
	env       Env
	opts      checkOptions
	editor    *editorconfig.Resolver
	rcs       *rcFinder
	ws        *workspace.Workspace
	resolveMu sync.Mutex
	resolved  map[string]error
	warned    sync.Map
	stdin     func() ([]byte, error)
	all       []target
	graphMu   sync.Mutex
	graphs    map[host.Language]*hostnav.Graph
}

func newChecker(env Env, opts checkOptions) *checker {
	if opts.rc.mode == rcExplicit {
		opts.rc.path = absolute(env.Dir, opts.rc.path)
	}
	c := &checker{
		env:      env,
		opts:     opts,
		editor:   editorconfig.NewResolver(regularfile.ReadConfig, time.Now, time.Hour),
		rcs:      newRCFinder(opts.rc, regularfile.ReadConfig, env.Home, env.ConfigHome),
		stdin:    sync.OnceValues(func() ([]byte, error) { return io.ReadAll(env.Stdin) }),
		resolved: map[string]error{},
	}
	c.ws = workspace.New(func(string) (*workspace.File, bool) { return nil, false }, c.resolve)
	return c
}

func (c *checker) color() bool {
	switch c.opts.color {
	case colorAlways:
		return true
	case colorNever:
		return false
	case colorAuto:
	}
	return c.env.Terminal
}

type fileSettings struct {
	lint  filelint.Settings
	roots []string
}

func (c *checker) settings(dir string) (fileSettings, error) {
	rc, err := c.rcs.forDir(dir)
	if err != nil {
		return fileSettings{}, err
	}
	if rc.Path != "" {
		if _, warned := c.warned.LoadOrStore(rc.Path, true); !warned {
			warnUnknownOptional(c.env.Stderr, rc.Enabled, rc.Path)
		}
	}
	s := fileSettings{lint: filelint.Settings{
		RC:       rc,
		Shell:    c.opts.shell,
		Exclude:  c.opts.exclude,
		Optional: c.opts.enable,
		Profile:  c.opts.profile,
		Extended: c.opts.extended,
	}}
	for _, p := range slices.Concat(c.opts.sourcePaths, rc.SourcePaths) {
		if p == directive.ScriptDir {
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

func (c *checker) resolve(path string, in dialect.Input) dialect.Detection {
	s, err := c.settings(filepath.Dir(path))
	if err != nil {
		c.remember(path, &configError{err})
		return dialect.Resolve(nil, in)
	}
	r, err := project.Resolve(c.editor, path, s.lint.Forced(), in)
	if err != nil {
		c.remember(path, &configError{err})
		return r.Detection
	}
	c.remember(path, nil)
	return r.Detection
}

func (c *checker) remember(path string, err error) {
	c.resolveMu.Lock()
	defer c.resolveMu.Unlock()
	c.resolved[path] = err
}

func (c *checker) resolution(path string) (error, bool) {
	c.resolveMu.Lock()
	defer c.resolveMu.Unlock()
	err, ok := c.resolved[path]
	return err, ok
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
	c.all = targets
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
	var operands []string
	for _, operand := range c.opts.operands {
		if operand != stdinName {
			operands = append(operands, operand)
			continue
		}
		if len(out) == 0 {
			out = append(out, target{name: stdinName})
		}
	}
	files, failures := collectTargets(c.env.Dir, operands, func(string) bool { return true })
	return append(out, files...), failures
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
	report := filelint.Check(file, env, s.lint)
	var syntax []lint.Finding
	for _, variant := range file.Variants() {
		syntax = append(syntax, syntaxFindings(variant.Tree, s.lint.DialectSource(variant))...)
	}
	findings := sourceFindings(file.Doc, c.filter(s.lint.Excluded(), append(syntax, report.Findings...)))
	if h, ok := hostnav.Of(file); ok {
		native, err := c.nativeFindings(h)
		if err != nil {
			return linted{failure: fmt.Sprintf("%s: %v", t.name, err)}
		}
		findings = c.filter(s.lint.Excluded(), append(findings, native...))
	}
	return linted{
		report: fileReport{name: t.name, doc: file.Doc.Source(), findings: findings},
		file:   file,
		env:    env,
	}
}

func (c *checker) nativeFindings(h hostnav.Host) ([]lint.Finding, error) {
	env := hostnav.Env{Loader: c.ws, Home: c.env.Home}
	env.Graph = func(ctx context.Context) (*hostnav.Graph, error) { return c.graph(ctx, h.Language(), env) }
	g, err := hostnav.Build(context.Background(), h, env)
	if err != nil {
		return nil, err
	}
	return g.Findings(), nil
}

func (c *checker) graph(ctx context.Context, language host.Language, env hostnav.Env) (*hostnav.Graph, error) {
	c.graphMu.Lock()
	defer c.graphMu.Unlock()
	if g, ok := c.graphs[language]; ok {
		return g, nil
	}
	g, err := hostnav.NewGraph(ctx, language, c.hostFiles(language), env)
	if err != nil {
		return nil, err
	}
	if c.graphs == nil {
		c.graphs = map[host.Language]*hostnav.Graph{}
	}
	c.graphs[language] = g
	return g, nil
}

func (c *checker) hostFiles(language host.Language) navigation.WorkspaceFiles {
	return func(ctx context.Context) ([]*workspace.File, error) {
		var out []*workspace.File
		for _, t := range c.all {
			if l, ok := host.FromName(t.path); ok && l == language {
				if f, err := c.ws.Load(t.path); err == nil {
					out = append(out, f)
				}
			}
		}
		return out, ctx.Err()
	}
}

type configError struct {
	err error
}

func (e *configError) Error() string { return e.err.Error() }

func (c *checker) load(t target) (*workspace.File, fileSettings, error) {
	dir := c.env.Dir
	if t.path != "" {
		dir = filepath.Dir(t.path)
	}
	s, err := c.settings(dir)
	if err != nil {
		return nil, fileSettings{}, &configError{err}
	}
	file, err := c.parse(t, s)
	if err != nil {
		return nil, fileSettings{}, err
	}
	return file, s, nil
}

func (c *checker) parse(t target, s fileSettings) (*workspace.File, error) {
	if t.path == "" {
		raw, err := c.stdin()
		if err != nil {
			return nil, fmt.Errorf("reading standard input: %w", err)
		}
		file, _, err := parseText("", string(raw), func(in dialect.Input) (project.Resolution, error) {
			return project.Resolve(c.editor, "", s.lint.Forced(), in)
		})
		if err != nil {
			return nil, &configError{err}
		}
		return file, nil
	}
	file, err := c.ws.Load(t.path)
	if err != nil {
		return nil, err
	}
	if file.Doc.Projected() {
		return file, nil
	}
	resolveErr, asked := c.resolution(t.path)
	if !asked {
		return nil, fmt.Errorf("resolving the dialect of %s: the workspace parsed it without asking", t.path)
	}
	if resolveErr != nil {
		return nil, resolveErr
	}
	return file, nil
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

func sourceFindings(doc *docstore.Document, findings []lint.Finding) []lint.Finding {
	if !doc.Projected() {
		return findings
	}
	out := make([]lint.Finding, len(findings))
	for i, f := range findings {
		f.Span = parser.Span{Start: doc.SourceOffset(f.Span.Start), End: doc.SourceOffset(f.Span.End)}
		f.Fixes = nil
		for _, fix := range findings[i].Fixes {
			edits, ok := doc.SourceEdits(fix.Edits)
			if !ok {
				panic("cli: filelint kept a fix that cannot be written in " + doc.URI)
			}
			fix.Edits = edits
			f.Fixes = append(f.Fixes, fix)
		}
		out[i] = f
	}
	return out
}

func (c *checker) filter(exclude lint.CodeSet, findings []lint.Finding) []lint.Finding {
	kept := slices.DeleteFunc(findings, func(f lint.Finding) bool {
		return f.Severity > c.opts.severity || exclude.Has(f.Code) || (c.opts.include != nil && !c.opts.include.Has(f.Code))
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
