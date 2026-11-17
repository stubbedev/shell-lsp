package main

import (
	"os"
	"path/filepath"
	"testing"
)

const repositoryRoot = "../.."

func TestReadmePerformanceSectionMatchesTheCommittedReport(t *testing.T) {
	t.Parallel()

	r, err := readReport(filepath.Join(repositoryRoot, committedReport))
	if err != nil {
		t.Fatal(err)
	}
	want, err := renderReadme(r)
	if err != nil {
		t.Fatal(err)
	}
	readme, err := os.ReadFile(filepath.Join(repositoryRoot, readmeFile))
	if err != nil {
		t.Fatal(err)
	}
	_, got, _, err := splitSection(string(readme))
	if err != nil {
		t.Fatalf("%s: %v", readmeFile, err)
	}
	if got != want {
		t.Errorf("the Performance section of %s does not match %s; run `just perf-readme %s`\n got:\n%s\nwant:\n%s", readmeFile, committedReport, committedReport, got, want)
	}
}
