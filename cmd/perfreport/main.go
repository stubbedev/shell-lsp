package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/atomicfile"
	"github.com/stubbedev/shell-lsp/internal/corpus"
)

const usage = `usage:
  perfreport run -runner LABEL -out FILE [-repeats N] [-nixpkgs NAME] [-references LIST] [-manifest FILE] [-cache DIR]
  perfreport readme REPORT
  perfreport notes REPORT`

const (
	defaultRepeats    = 5
	defaultNixpkgs    = "nixpkgs"
	defaultReferences = "nixd,nil"
	committedReport   = "corpus/perf.json"
	readmeFile        = "README.md"
)

var errUsage = errors.New(usage)

var subcommands = map[string]func(ctx context.Context, args []string) error{
	"run":    runMeasure,
	"readme": runReadme,
	"notes":  runNotes,
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
		fmt.Fprintln(os.Stderr, "perfreport:", err)
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

type runOptions struct {
	corpus.Source
	runner     string
	out        string
	nixpkgs    string
	references []string
	repeats    int
}

func parseRun(args []string) (runOptions, error) {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	var o runOptions
	o.Register(fs, "corpus/nix/repos.txt", "corpus-nix")
	fs.StringVar(&o.runner, "runner", "", "what the measurements run on, such as \"GitHub Actions ubuntu24\"")
	fs.StringVar(&o.out, "out", "", "file the JSON report is written to")
	fs.StringVar(&o.nixpkgs, "nixpkgs", defaultNixpkgs, "manifest entry whose scripts and Nix files are checked and formatted, and which NIX_PATH names")
	references := fs.String("references", defaultReferences, "comma-separated Nix servers whose memory is compared")
	fs.IntVar(&o.repeats, "repeats", defaultRepeats, "runs of each timed measurement; the report keeps the median")
	if err := fs.Parse(args); err != nil {
		return runOptions{}, errUsage
	}
	o.references = strings.Split(*references, ",")
	if fs.NArg() != 0 || o.runner == "" || o.out == "" || o.repeats <= 0 || o.nixpkgs == "" || *references == "" {
		return runOptions{}, errUsage
	}
	return o, nil
}

func runMeasure(ctx context.Context, args []string) error {
	o, err := parseRun(args)
	if err != nil {
		return err
	}
	r, err := measureAll(ctx, o)
	if err != nil {
		return err
	}
	raw, err := encodeReport(r)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(o.out), 0o750); err != nil {
		return fmt.Errorf("creating the directory of %s: %w", o.out, err)
	}
	return atomicfile.Write(o.out, raw)
}

func singleReport(args []string) (report, error) {
	if len(args) != 1 {
		return report{}, errUsage
	}
	return readReport(args[0])
}

func runReadme(_ context.Context, args []string) error {
	r, err := singleReport(args)
	if err != nil {
		return err
	}
	raw, err := encodeReport(r)
	if err != nil {
		return err
	}
	readme, err := os.ReadFile(readmeFile)
	if err != nil {
		return fmt.Errorf("reading %s: %w", readmeFile, err)
	}
	section, err := renderReadme(r)
	if err != nil {
		return err
	}
	updated, err := replaceSection(string(readme), section)
	if err != nil {
		return fmt.Errorf("%s: %w", readmeFile, err)
	}
	if err := atomicfile.Write(committedReport, raw); err != nil {
		return err
	}
	return atomicfile.Write(readmeFile, []byte(updated))
}

func runNotes(_ context.Context, args []string) error {
	r, err := singleReport(args)
	if err != nil {
		return err
	}
	notes, err := renderNotes(r)
	if err != nil {
		return err
	}
	_, err = fmt.Print(notes)
	return err
}
