package main

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/stubbedev/shell-lsp/internal/atomicfile"
	"github.com/stubbedev/shell-lsp/internal/corpus"
	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parallel"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

const (
	shellsBaselineFile = "corpus/shells.txt"
	shellTimeout       = time.Minute
)

type realShell uint8

const (
	bashShell realShell = iota
	dashShell
	zshShell
	kshShell
	mkshShell
	busyboxShell
)

type realShellSpec struct {
	name        string
	argv        []string
	syntaxError string
	ours        dialect.Shell
}

var realShells = [...]realShellSpec{
	bashShell:    {name: "bash", argv: []string{"bash", "-O", "extglob", "-n"}, ours: dialect.Shell{Dialect: dialect.Bash}},
	dashShell:    {name: "dash", argv: []string{"dash", "-n"}, ours: dialect.Shell{Dialect: dialect.Dash}},
	zshShell:     {name: "zsh", argv: []string{"zsh", "-f", "-n"}, syntaxError: "parse error", ours: dialect.Shell{Dialect: dialect.Zsh}},
	kshShell:     {name: "ksh", argv: []string{"ksh", "-n"}, ours: dialect.Shell{Dialect: dialect.Ksh}},
	mkshShell:    {name: "mksh", argv: []string{"mksh", "-n"}, ours: dialect.FlavorMksh.Shell()},
	busyboxShell: {name: "busybox", argv: []string{"busybox", "sh", "-n"}, ours: dialect.Shell{Dialect: dialect.Busybox}},
}

const oilsBusyboxName = "ash"

func (s realShell) String() string { return realShells[s].name }

func realShellNames() []string {
	names := make([]string, len(realShells))
	for i, spec := range realShells {
		names[i] = spec.name
	}
	return names
}

func oilsShell(name string) (realShell, bool) {
	if name == oilsBusyboxName {
		return busyboxShell, true
	}
	return named[realShell](realShellNames(), name)
}

func realShellFor(s dialect.Shell) (realShell, bool) {
	switch s.Dialect {
	case dialect.Bash:
		return bashShell, !s.Bats()
	case dialect.Posix, dialect.Dash:
		return dashShell, true
	case dialect.Zsh:
		return zshShell, true
	case dialect.Ksh:
		if s.Flavor == dialect.FlavorMksh {
			return mkshShell, true
		}
		return kshShell, true
	case dialect.Busybox:
		return busyboxShell, true
	}
	return 0, false
}

type parseItem struct {
	id    string
	text  string
	shell realShell
	ours  dialect.Shell
}

type verdict uint8

const (
	shellRejects verdict = iota
	parserRejects
)

var verdictNames = [...]string{shellRejects: "shell-rejects", parserRejects: "parser-rejects"}

func (v verdict) String() string { return verdictNames[v] }

type difference struct {
	id      string
	shell   realShell
	verdict verdict
	detail  string
}

func (d difference) key() string { return d.shell.String() + " " + d.verdict.String() + " " + d.id }

func cmpDifference(a, b difference) int {
	if n := strings.Compare(a.id, b.id); n != 0 {
		return n
	}
	return cmp.Compare(a.shell, b.shell)
}

func runShells(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("shells", flag.ContinueOnError)
	var c corpusFlags
	c.register(fs)
	baselineFile := fs.String("baseline", shellsBaselineFile, "baseline to compare with or rewrite")
	update := fs.Bool("update", false, "rewrite the baseline instead of comparing with it")
	explain := fs.Bool("explain", false, "print each difference with the shell's message or the parser's first error")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errUsage
	}
	repos, err := c.fetched()
	if err != nil {
		return err
	}
	items, err := parseItems(repos, c.cache)
	if err != nil {
		return err
	}
	got, err := differences(ctx, items)
	if err != nil {
		return err
	}
	if *explain {
		for _, d := range got {
			fmt.Printf("%s: %s\n", d.key(), d.detail)
		}
	}
	want, err := readShellsBaseline(*baselineFile, *update)
	if err != nil {
		return err
	}
	if *update {
		return atomicfile.Write(*baselineFile, []byte(renderShellsBaseline(reclassify(want, len(items), got))))
	}
	problems := diffShells(want, len(items), got)
	if len(problems) == 0 {
		fmt.Printf("%d parses match %s\n", len(items), *baselineFile)
		return nil
	}
	for _, p := range problems {
		fmt.Println(p)
	}
	return fmt.Errorf("the parser no longer matches %s; fix the new differences, or run `just corpus-baseline`, classify the new entries and review the diff", *baselineFile)
}

func readShellsBaseline(file string, missingOK bool) (shellsBaseline, error) {
	raw, err := os.ReadFile(file) //nolint:gosec
	if errors.Is(err, os.ErrNotExist) && missingOK {
		return shellsBaseline{}, nil
	}
	if err != nil {
		return shellsBaseline{}, fmt.Errorf("reading the shells baseline: %w", err)
	}
	b, err := parseShellsBaseline(string(raw))
	if err != nil {
		return shellsBaseline{}, fmt.Errorf("%s: %w", file, err)
	}
	return b, nil
}

func parseItems(repos []corpus.Repo, cache string) ([]parseItem, error) {
	listing, err := corpus.Scripts(repos, cache)
	if err != nil {
		return nil, err
	}
	cases, err := corpus.SpecCases(repos, cache)
	if err != nil {
		return nil, err
	}
	var items []parseItem
	for _, s := range listing.Scripts {
		if shell, ok := realShellFor(s.Shell); ok {
			items = append(items, parseItem{id: s.Path, text: s.Text, shell: shell, ours: s.Shell})
		}
	}
	for _, c := range cases {
		for _, name := range c.Shells {
			if shell, ok := oilsShell(name); ok {
				items = append(items, parseItem{id: c.ID(), text: c.Code, shell: shell, ours: realShells[shell].ours})
			}
		}
	}
	return items, nil
}

func differences(ctx context.Context, items []parseItem) ([]difference, error) {
	type result struct {
		diff *difference
		err  error
	}
	results := make([]result, len(items))
	parallel.For(runtime.NumCPU(), len(items), func(i int) {
		it := items[i]
		accepted, message, err := runShell(ctx, it.shell, it.text)
		if err != nil {
			results[i] = result{err: fmt.Errorf("%s with %s: %w", it.id, it.shell, err)}
			return
		}
		errs := parser.ParseEdit(nil, it.text, it.ours).Errors
		switch {
		case !accepted && len(errs) == 0:
			results[i] = result{diff: &difference{id: it.id, shell: it.shell, verdict: shellRejects, detail: message}}
		case accepted && len(errs) > 0:
			results[i] = result{diff: &difference{id: it.id, shell: it.shell, verdict: parserRejects, detail: describeError(it.text, errs[0])}}
		}
	})
	var out []difference
	var errs []error
	for _, r := range results {
		if r.err != nil {
			errs = append(errs, r.err)
		}
		if r.diff != nil {
			out = append(out, *r.diff)
		}
	}
	slices.SortFunc(out, cmpDifference)
	return out, errors.Join(errs...)
}

func describeError(text string, e parser.Error) string {
	start := strings.LastIndexByte(text[:e.Span.Start], '\n') + 1
	end := len(text)
	if i := strings.IndexByte(text[start:], '\n'); i >= 0 {
		end = start + i
	}
	return fmt.Sprintf("line %d: %s: %s", strings.Count(text[:start], "\n")+1, e.Message, strings.TrimSpace(text[start:end]))
}

func runShell(ctx context.Context, shell realShell, text string) (accepted bool, message string, err error) {
	ctx, cancel := context.WithTimeout(ctx, shellTimeout)
	defer cancel()
	spec := realShells[shell]
	cmd := exec.CommandContext(ctx, spec.argv[0], spec.argv[1:]...) //nolint:gosec
	cmd.Stdin = strings.NewReader(text)
	cmd.Env = isolatedEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	message, _, _ = strings.Cut(stderr.String(), "\n")
	if runErr == nil {
		return true, message, nil
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](runErr); ok && exitErr.ExitCode() > 0 && ctx.Err() == nil {
		return spec.syntaxError != "" && !strings.Contains(stderr.String(), spec.syntaxError), message, nil
	}
	return false, "", fmt.Errorf("%s: %w: %s", strings.Join(spec.argv, " "), runErr, strings.TrimSpace(stderr.String()))
}
