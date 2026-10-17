package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/stubbedev/shell-lsp/internal/atomicfile"
	"github.com/stubbedev/shell-lsp/internal/corpus"
	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/host/oracle"
	"github.com/stubbedev/shell-lsp/internal/hostnav"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

var makeReadCodes = []lint.Code{lint.CodeMakeSyntax, lint.CodeMakeConditional, lint.CodeMakeDefine, lint.CodeMakeRecipePrefix, lint.CodeMakeRecipeOutsideRule}

const (
	hostsBaselineFile = "corpus/hosts.txt"
	slowHostFile      = 2 * time.Second
)

type hostReport struct {
	path    string
	lines   []string
	elapsed time.Duration
	judged  bool
}

func runHosts(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("hosts", flag.ContinueOnError)
	var c corpusFlags
	c.register(fs)
	baselineFile := fs.String("baseline", hostsBaselineFile, "baseline to compare with or rewrite")
	update := fs.Bool("update", false, "rewrite the baseline instead of comparing with it")
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
	files, err := corpus.Hosts(repos, c.cache)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return errors.New("the corpus lists no justfiles or Makefiles; is a `hosts` repository in the manifest?")
	}
	reports, err := analyseHosts(ctx, files)
	if err != nil {
		return err
	}
	var slow []string
	var sb strings.Builder
	for _, r := range reports {
		for _, line := range r.lines {
			sb.WriteString(r.path + ":" + line + "\n")
		}
		if r.elapsed > slowHostFile {
			slow = append(slow, fmt.Sprintf("%s took %s", r.path, r.elapsed.Round(time.Millisecond)))
		}
	}
	if len(slow) > 0 {
		return fmt.Errorf("analysing the corpus is too slow:\n%s", strings.Join(slow, "\n"))
	}
	got := sb.String()
	if *update {
		return atomicfile.Write(*baselineFile, []byte(got))
	}
	raw, err := os.ReadFile(*baselineFile)
	if err != nil {
		return fmt.Errorf("reading the hosts baseline: %w", err)
	}
	judged := 0
	for _, r := range reports {
		if r.judged {
			judged++
		}
	}
	if string(raw) == got {
		fmt.Printf("%d justfiles and Makefiles match %s; make judged %d of the Makefiles\n", len(reports), *baselineFile, judged)
		return nil
	}
	for _, line := range diffLines(string(raw), got) {
		fmt.Println(line)
	}
	return fmt.Errorf("the analysis of the corpus justfiles and Makefiles no longer matches %s; fix the change, or run `just corpus-baseline` and review the diff", *baselineFile)
}

func diffLines(want, got string) []string {
	wanted := strings.Split(strings.TrimSuffix(want, "\n"), "\n")
	have := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	inWanted, inHave := lineSet(wanted), lineSet(have)
	var out []string
	for _, line := range wanted {
		if !inHave[line] {
			out = append(out, "- "+line)
		}
	}
	for _, line := range have {
		if !inWanted[line] {
			out = append(out, "+ "+line)
		}
	}
	return out
}

func lineSet(lines []string) map[string]bool {
	out := make(map[string]bool, len(lines))
	for _, line := range lines {
		out[line] = true
	}
	return out
}

func analyseHosts(ctx context.Context, files []corpus.HostFile) ([]hostReport, error) {
	byRoot := map[string]*workspace.Workspace{}
	graphs := map[string]*hostnav.Graph{}
	var out []hostReport
	for _, f := range files {
		ws, ok := byRoot[f.Root]
		if !ok {
			ws = workspace.New(func(string) (*workspace.File, bool) { return nil, false }, workspace.Detect)
			ws.SetRoots([]string{f.Root})
			byRoot[f.Root] = ws
		}
		file, err := ws.LoadAs(f.File, f.Language)
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", f.Path, err)
		}
		h, ok := hostnav.Of(file)
		if !ok {
			return nil, fmt.Errorf("%s has no native analysis", f.Path)
		}
		env := hostnav.Env{Loader: ws}
		key := f.Root + "\x00" + f.Language.String()
		env.Graph = func(ctx context.Context) (*hostnav.Graph, error) {
			if g, ok := graphs[key]; ok {
				return g, nil
			}
			g, err := hostnav.NewGraph(ctx, f.Language, sameLanguage(ws, files, f), env)
			graphs[key] = g
			return g, err
		}
		start := time.Now()
		g, err := hostnav.Build(ctx, h, env)
		if err != nil {
			return nil, fmt.Errorf("analysing %s: %w", f.Path, err)
		}
		findings := g.Findings()
		r := hostReport{path: f.Path, elapsed: time.Since(start)}
		text := file.Doc.Source().Text()
		var errorLines, readLines []int
		for _, finding := range sortedFindings(findings) {
			line, column := lineColumn(text, finding.Span.Start)
			r.lines = append(r.lines, fmt.Sprintf("%d:%d %s", line, column, finding.Code))
			if finding.Severity == lint.SeverityError {
				errorLines = append(errorLines, line)
			}
			if slices.Contains(makeReadCodes, finding.Code) {
				readLines = append(readLines, line)
			}
		}
		lines, _ := lineColumn(text, len(text))
		switch {
		case f.Language == host.Just:
			if problem, disagrees := oracle.Disagreement(oracle.Just(ctx, f.File, filepath.Dir(f.File), isolatedEnv()), lines, errorLines); disagrees {
				r.lines = append(r.lines, "just disagrees: "+problem)
			}
		case f.Language == host.Make && host.Make.EntryPoint(f.File):
			verdict := oracle.Make(ctx, f.File, f.Root, text, isolatedEnv())
			r.judged = !verdict.Inconclusive
			if problem, disagrees := oracle.Disagreement(verdict, lines, readLines); disagrees {
				r.lines = append(r.lines, "make disagrees: "+problem)
			}
		}
		out = append(out, r)
	}
	return out, nil
}

func sameLanguage(ws *workspace.Workspace, files []corpus.HostFile, self corpus.HostFile) func(context.Context) ([]*workspace.File, error) {
	return func(context.Context) ([]*workspace.File, error) {
		var out []*workspace.File
		for _, f := range files {
			if f.Root != self.Root || f.Language != self.Language {
				continue
			}
			file, err := ws.LoadAs(f.File, f.Language)
			if err != nil {
				return nil, fmt.Errorf("loading %s: %w", f.Path, err)
			}
			out = append(out, file)
		}
		return out, nil
	}
}

func sortedFindings(findings []lint.Finding) []lint.Finding {
	out := slices.Clone(findings)
	slices.SortStableFunc(out, func(a, b lint.Finding) int {
		return cmp.Or(cmp.Compare(a.Span.Start, b.Span.Start), cmp.Compare(a.Code, b.Code))
	})
	return out
}

func lineColumn(text string, offset int) (int, int) {
	return strings.Count(text[:offset], "\n") + 1, offset - (strings.LastIndexByte(text[:offset], '\n') + 1) + 1
}
