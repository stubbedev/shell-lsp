package parser

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestReadmeListsEveryCommandWrapper(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(strings.Fields(string(raw)), " ")
	_, list, found := strings.Cut(text, "commands behind the wrappers ")
	list, _, ended := strings.Cut(list, " complete and hover")
	if !found || !ended {
		t.Fatal("README.md no longer lists the command wrappers completion and hover look through")
	}
	var got []string
	for _, m := range regexp.MustCompile("`([^`]+)`").FindAllStringSubmatch(list, -1) {
		got = append(got, m[1])
	}
	if want := slices.Sorted(maps.Keys(commandWrappers)); !slices.Equal(got, want) {
		t.Errorf("README.md lists the wrappers %v, the parser unwraps %v", got, want)
	}
}
