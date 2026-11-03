package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/corpus"
	"github.com/stubbedev/shell-lsp/internal/host"
)

func writeRecipeCorpus(t *testing.T) []corpus.HostFile {
	t.Helper()
	root := t.TempDir()
	files := []corpus.HostFile{
		{Repo: "repo", Root: root, Path: "repo/Makefile", File: filepath.Join(root, "Makefile"), Language: host.Make},
		{Repo: "repo", Root: root, Path: "repo/justfile", File: filepath.Join(root, "justfile"), Language: host.Just},
	}
	texts := []string{
		"export token := a b\nall:\n\tcp $$token /tmp\n\tcd /tmp\n",
		"run $target:\n    cp $target /tmp\n",
	}
	for i, f := range files {
		if err := os.WriteFile(f.File, []byte(texts[i]), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return files
}

func TestRecipeFindingsAreRenderedByRepositoryPathAndPosition(t *testing.T) {
	t.Parallel()

	files := writeRecipeCorpus(t)
	findings, err := recipeFindings(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	got := renderRecipes(len(files), findings)
	if strings.Contains(got, files[0].Root) {
		t.Errorf("the baseline names the machine's directory:\n%s", got)
	}
	_, list, ok := strings.Cut(got, "## Findings\n\n")
	if !ok {
		t.Fatalf("no findings section:\n%s", got)
	}
	want := []string{"repo/Makefile:3:5 SC2086", "repo/Makefile:4:2 SC2164", "repo/justfile:2:8 SC2086"}
	if lines := strings.Split(strings.TrimSpace(list), "\n"); !slices.Equal(lines, want) {
		t.Errorf("findings = %q, want %q", lines, want)
	}
	if !strings.Contains(got, "2 files checked, 3 findings.") || !strings.Contains(got, "| SC2086 | 2 |\n| SC2164 | 1 |\n") {
		t.Errorf("summary is wrong:\n%s", got)
	}
	again, err := recipeFindings(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	if rendered := renderRecipes(len(files), again); rendered != got {
		t.Errorf("a second run differs:\n%s\n---\n%s", rendered, got)
	}
}

func TestBaselineComparisonFailsOnAChange(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "baseline.md")
	baseline := textBaseline{file: file, got: "a\n", name: "test", matches: "matches", stale: "changed", rewrite: "rewrite"}
	if err := checkBaseline(baseline); err == nil {
		t.Error("a missing baseline passes")
	}
	written := baseline
	written.update = true
	if err := checkBaseline(written); err != nil {
		t.Fatal(err)
	}
	if err := checkBaseline(baseline); err != nil {
		t.Errorf("the baseline just written does not match: %v", err)
	}
	changed := baseline
	changed.got = "b\n"
	if err := checkBaseline(changed); err == nil {
		t.Error("a changed finding passes")
	}
}
