package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/stubbedev/shell-lsp/internal/corpus"
)

const usage = `usage:
  lintcheck compare [-examples N] [-code SCxxxx] PATH...
  lintcheck fetch [-manifest FILE] [-cache DIR]
  lintcheck shellcheck [-manifest FILE] [-cache DIR] [-baseline FILE] [-update]`

var errUsage = errors.New(usage)

var subcommands = map[string]func(ctx context.Context, args []string) error{
	"compare":    runCompare,
	"fetch":      runFetch,
	"shellcheck": runShellcheck,
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
		fmt.Fprintln(os.Stderr, "lintcheck:", err)
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

func isolatedEnv() []string {
	return []string{"PATH=" + os.Getenv("PATH"), "LC_ALL=C.UTF-8", "HOME=/nonexistent"}
}

type corpusFlags struct {
	manifest, cache string
}

func defaultCache() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "shell-lsp", "corpus")
}

func (c *corpusFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&c.manifest, "manifest", "corpus/repos.txt", "corpus manifest")
	fs.StringVar(&c.cache, "cache", defaultCache(), "directory the corpus is fetched into")
}

func (c corpusFlags) repos() ([]corpus.Repo, error) {
	if c.cache == "" {
		return nil, errors.New("no cache directory: pass -cache, or set HOME or XDG_CACHE_HOME")
	}
	return corpus.LoadManifest(c.manifest)
}

func (c corpusFlags) fetched() ([]corpus.Repo, error) {
	repos, err := c.repos()
	if err != nil {
		return nil, err
	}
	return repos, corpus.Fetched(repos, c.cache)
}

func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return errUsage
	}
	return nil
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
