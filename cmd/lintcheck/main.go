package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/stubbedev/shell-lsp/internal/corpus"
)

const usage = `usage:
  lintcheck compare [-examples N] [-code SCxxxx] PATH...
  lintcheck fetch [-manifest FILE] [-cache DIR]
  lintcheck hosts [-manifest FILE] [-cache DIR] [-baseline FILE] [-update]
  lintcheck recipes [-manifest FILE] [-cache DIR] [-baseline FILE] [-update]
  lintcheck nixlint [-nixpkgs DIR] [-sample N] [-baseline FILE] [-update] [-examples N] [-skip NAMES] [PATH...]
  lintcheck shellcheck [-manifest FILE] [-cache DIR] [-baseline FILE] [-update]
  lintcheck shells [-manifest FILE] [-cache DIR] [-baseline FILE] [-update] [-explain]`

var errUsage = errors.New(usage)

var subcommands = map[string]func(ctx context.Context, args []string) error{
	"compare":    runCompare,
	"nixlint":    runNixLint,
	"fetch":      runFetch,
	"hosts":      runHosts,
	"recipes":    runRecipes,
	"shellcheck": runShellcheck,
	"shells":     runShells,
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

func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return errUsage
	}
	return nil
}

func runFetch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	var c corpus.Source
	registerCorpus(fs, &c)
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errUsage
	}
	_, err := c.Fetch(ctx)
	return err
}

func registerCorpus(fs *flag.FlagSet, c *corpus.Source) {
	c.Register(fs, "corpus/repos.txt", "corpus")
}
