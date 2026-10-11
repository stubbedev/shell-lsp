package actions

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestReadmeListsEveryRefactoring(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, table, found := strings.Cut(string(raw), "| Action | Kind | What it does |\n")
	if !found {
		t.Fatal("README.md has no refactoring table")
	}
	table, _, _ = strings.Cut(table, "\n\n")
	var documented []string
	for row := range strings.SplitSeq(table, "\n") {
		cells := strings.Split(row, " | ")
		if strings.HasPrefix(row, "| ---") || len(cells) < 2 {
			continue
		}
		documented = append(documented, strings.Trim(cells[1], "`"))
	}
	implemented := make([]string, len(refactorings))
	for i, r := range refactorings {
		implemented[i] = string(r.action)
	}
	slices.Sort(documented)
	slices.Sort(implemented)
	if !slices.Equal(documented, implemented) {
		t.Errorf("README.md documents refactorings of kinds %v, the server offers %v", documented, implemented)
	}
}
