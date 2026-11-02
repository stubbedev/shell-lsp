package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/stubbedev/shell-lsp/internal/atomicfile"
	"github.com/stubbedev/shell-lsp/internal/corpus"
)

const usage = `usage:
  nixparity fetch [-manifest FILE] [-cache DIR]
  nixparity compare [-ours ARGV] [-theirs ARGV] [-seed N] [-positions N] [-files N] [-timeout D]
                    [-examples N] [-out PREFIX] [-baseline FILE] [-update]
                    [-repo NAME [-manifest FILE] [-cache DIR] | DIR]`

const (
	defaultManifest  = "corpus/nix/repos.txt"
	oursName         = "shell-lsp"
	defaultExamples  = 5
	defaultPositions = 20
	defaultFiles     = 200
	defaultTimeout   = 30 * time.Second
)

var errUsage = errors.New(usage)

var subcommands = map[string]func(ctx context.Context, args []string) error{
	"compare": runCompare,
	"fetch":   runFetch,
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := dispatch(ctx, os.Args[1:])
	stop()
	switch {
	case errors.Is(err, errUsage):
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	case err != nil:
		fmt.Fprintln(os.Stderr, "nixparity:", err)
		os.Exit(1)
	}
}

func dispatch(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	sub, ok := subcommands[args[0]]
	if !ok {
		return errUsage
	}
	return sub(ctx, args[1:])
}

func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return errUsage
	}
	return nil
}

type corpusFlags struct {
	manifest, cache string
}

func defaultCache() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "shell-lsp", "corpus-nix")
}

func (c *corpusFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&c.manifest, "manifest", defaultManifest, "manifest of the large flakes")
	fs.StringVar(&c.cache, "cache", defaultCache(), "directory the large flakes are fetched into")
}

func (c corpusFlags) repos() ([]corpus.Repo, error) {
	if c.cache == "" {
		return nil, errors.New("no cache directory: pass -cache, or set HOME or XDG_CACHE_HOME")
	}
	return corpus.LoadManifest(c.manifest)
}

func runFetch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	var c corpusFlags
	c.register(fs)
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errUsage
	}
	repos, err := c.repos()
	if err != nil {
		return err
	}
	return corpus.Fetch(ctx, repos, c.cache)
}

func (c corpusFlags) repoDir(name string) (string, error) {
	repos, err := c.repos()
	if err != nil {
		return "", err
	}
	for _, r := range repos {
		if r.Name != name {
			continue
		}
		dir := r.Dir(c.cache)
		if _, err := os.Stat(dir); err != nil {
			return "", fmt.Errorf("%s is not fetched; run `just nix-parity-fetch` first: %w", name, err)
		}
		return dir, nil
	}
	return "", fmt.Errorf("%s does not list %s", c.manifest, name)
}

type compareOptions struct {
	corpusFlags
	ours, theirs string
	repo         string
	settings     settings
	timeout      time.Duration
	examples     int
	out          string
	baseline     string
	update       bool
}

func parseCompare(args []string) (compareOptions, string, error) {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	var o compareOptions
	o.register(fs)
	fs.StringVar(&o.ours, "ours", "", "command line of the server under test; built from this repository when empty")
	fs.StringVar(&o.theirs, "theirs", "nixd", "command line of the reference server, such as nixd or nil")
	fs.StringVar(&o.repo, "repo", "", "fetched manifest entry to measure instead of a directory")
	fs.Uint64Var(&o.settings.seed, "seed", 1, "seed of the file and position sampling")
	fs.IntVar(&o.settings.positions, "positions", defaultPositions, "positions sampled per file")
	fs.IntVar(&o.settings.fileLimit, "files", defaultFiles, "files sampled from the project")
	fs.DurationVar(&o.timeout, "timeout", defaultTimeout, "limit on every server call")
	fs.IntVar(&o.examples, "examples", defaultExamples, "examples kept per feature and verdict")
	fs.StringVar(&o.out, "out", "", "also write the report to PREFIX.md and PREFIX.json")
	fs.StringVar(&o.baseline, "baseline", "", "counts file to compare with or rewrite")
	fs.BoolVar(&o.update, "update", false, "rewrite the baseline instead of comparing with it")
	if err := parseFlags(fs, args); err != nil {
		return compareOptions{}, "", err
	}
	if o.settings.positions <= 0 || o.settings.fileLimit <= 0 || o.timeout <= 0 || o.examples < 0 || (o.update && o.baseline == "") {
		return compareOptions{}, "", errUsage
	}
	switch {
	case o.repo != "" && fs.NArg() == 0:
		dir, err := o.repoDir(o.repo)
		return o, dir, err
	case o.repo == "" && fs.NArg() == 1:
		return o, fs.Arg(0), nil
	}
	return compareOptions{}, "", errUsage
}

func serverArgv(name, commandLine string) ([]string, error) {
	argv := strings.Fields(commandLine)
	if len(argv) == 0 {
		return nil, fmt.Errorf("the %s command line is empty", name)
	}
	return argv, nil
}

func buildOurs(ctx context.Context, dir string) (string, error) {
	bin := filepath.Join(dir, oursName)
	cmd := exec.CommandContext(ctx, "go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("building %s from the working directory: %w\n%s", oursName, err, out)
	}
	return bin, nil
}

func resolveRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", dir, err)
	}
	root, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", dir, err)
	}
	return root, nil
}

func runCompare(ctx context.Context, args []string) error {
	o, dir, err := parseCompare(args)
	if err != nil {
		return err
	}
	root, err := resolveRoot(dir)
	if err != nil {
		return err
	}
	if o.ours == "" {
		tmp, err := os.MkdirTemp("", "nixparity-")
		if err != nil {
			return fmt.Errorf("creating a directory for the %s build: %w", oursName, err)
		}
		defer os.RemoveAll(tmp) //nolint:errcheck
		if o.ours, err = buildOurs(ctx, tmp); err != nil {
			return err
		}
	}
	oursArgv, err := serverArgv("ours", o.ours)
	if err != nil {
		return err
	}
	theirsArgv, err := serverArgv("theirs", o.theirs)
	if err != nil {
		return err
	}
	p, err := buildPlan(root, o.settings)
	if err != nil {
		return err
	}
	ours, err := measure(ctx, serverConfig{name: filepath.Base(oursArgv[0]), argv: oursArgv, root: root, timeout: o.timeout}, p)
	if err != nil {
		return err
	}
	theirs, err := measure(ctx, serverConfig{name: filepath.Base(theirsArgv[0]), argv: theirsArgv, root: root, timeout: o.timeout}, p)
	if err != nil {
		return err
	}
	r := compareRuns(p, ours, theirs, o.examples)
	markdown := renderMarkdown(r)
	fmt.Print(markdown)
	if err := writeOutputs(o.out, markdown, r); err != nil {
		return err
	}
	return checkBaseline(o, r)
}

func writeOutputs(prefix, markdown string, r report) error {
	if prefix == "" {
		return nil
	}
	raw, err := encodeJSON(r)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(prefix), 0o750); err != nil {
		return fmt.Errorf("creating the report directory of %s: %w", prefix, err)
	}
	if err := atomicfile.Write(prefix+".md", []byte(markdown)); err != nil {
		return err
	}
	return atomicfile.Write(prefix+".json", raw)
}

func printExamples(examples []example, f feature) {
	for _, e := range examples {
		if e.Feature == f {
			fmt.Println(e)
		}
	}
}

func checkBaseline(o compareOptions, r report) error {
	if o.baseline == "" {
		return nil
	}
	if o.update {
		raw, err := encodeJSON(r.Counts)
		if err != nil {
			return err
		}
		return atomicfile.Write(o.baseline, raw)
	}
	raw, err := os.ReadFile(o.baseline)
	if err != nil {
		return fmt.Errorf("reading the parity baseline: %w", err)
	}
	want, err := parseBaseline(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", o.baseline, err)
	}
	problems := diffCounts(want, r.Counts)
	if len(problems) == 0 {
		fmt.Printf("%s matches %s\n", r.Counts.Scope.Project, o.baseline)
		return nil
	}
	for _, p := range problems {
		fmt.Println(p.message)
		if p.regressed {
			printExamples(r.Examples, p.feature)
		}
	}
	return fmt.Errorf("%s no longer matches %s; fix the regressions, or run `just nix-parity-baseline` and review the diff", r.Counts.Scope.Project, o.baseline)
}
