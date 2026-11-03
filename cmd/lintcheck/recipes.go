package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/corpus"
	"github.com/stubbedev/shell-lsp/internal/filelint"
	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

const recipesBaselineFile = "corpus/recipes.md"

type recipeFinding struct {
	path      string
	line, col int
	code      lint.Code
}

func (f recipeFinding) String() string {
	return fmt.Sprintf("%s:%d:%d %s", f.path, f.line, f.col, f.code)
}

func runRecipes(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("recipes", flag.ContinueOnError)
	var c corpusFlags
	c.register(fs)
	baselineFile := fs.String("baseline", recipesBaselineFile, "baseline to compare with or rewrite")
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
	all, err := corpus.Hosts(repos, c.cache)
	if err != nil {
		return err
	}
	files := slices.DeleteFunc(all, func(f corpus.HostFile) bool { return f.Language != host.Just && f.Language != host.Make })
	if len(files) == 0 {
		return errors.New("the corpus lists no justfiles or Makefiles; is a `hosts` repository in the manifest?")
	}
	findings, err := recipeFindings(ctx, files)
	if err != nil {
		return err
	}
	return checkBaseline(textBaseline{
		file:    *baselineFile,
		got:     renderRecipes(len(files), findings),
		update:  *update,
		name:    "recipes",
		matches: fmt.Sprintf("%d justfiles and Makefiles match", len(files)),
		stale:   "the shell findings in the corpus justfiles and Makefiles no longer match",
		rewrite: "just corpus-baseline",
	})
}

func recipeFindings(ctx context.Context, files []corpus.HostFile) ([]recipeFinding, error) {
	byRoot := map[string]*workspace.Workspace{}
	var out []recipeFinding
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
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
		text := file.Doc.Source().Text()
		report := filelint.Check(file, navigation.Env{Loader: ws, Roots: []string{f.Root}}, filelint.Settings{})
		placed := make([]lint.Finding, len(report.Findings))
		for i, finding := range report.Findings {
			finding.Span.Start = file.Doc.SourceOffset(finding.Span.Start)
			placed[i] = finding
		}
		for _, finding := range sortedFindings(placed) {
			line, col := lineColumn(text, finding.Span.Start)
			out = append(out, recipeFinding{path: f.Path, line: line, col: col, code: finding.Code})
		}
	}
	return out, nil
}

func renderRecipes(files int, findings []recipeFinding) string {
	var sb strings.Builder
	sb.WriteString("# Shell findings in just and Make recipes\n\n")
	sb.WriteString("Written by `just corpus-baseline` from the justfiles and Makefiles of the repositories in\n")
	sb.WriteString("[repos.txt](repos.txt); see [docs/corpus.md](../docs/corpus.md). Positions and codes only, one\n")
	sb.WriteString("finding a line. CI fails when anything here changes.\n\n")
	fmt.Fprintf(&sb, "%d files checked, %d findings.\n\n", files, len(findings))
	sb.WriteString(tableRow("Code", "Findings") + "\n" + tableRow("---", "---:") + "\n")
	counts := map[lint.Code]int{}
	for _, f := range findings {
		counts[f.code]++
	}
	for _, code := range slices.Sorted(maps.Keys(counts)) {
		sb.WriteString(tableRow(code.String(), strconv.Itoa(counts[code])) + "\n")
	}
	sb.WriteString("\n## Findings\n\n")
	for _, f := range findings {
		sb.WriteString(f.String() + "\n")
	}
	return sb.String()
}
