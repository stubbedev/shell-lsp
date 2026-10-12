package dialect

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestReadmeNamesEverySource(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, row, found := strings.Cut(string(raw), "| `shellLsp/documentDialect` |")
	if !found {
		t.Fatal("README.md has no shellLsp/documentDialect row")
	}
	row, _, _ = strings.Cut(row, "\n")
	for _, name := range sourceNames {
		if !strings.Contains(row, "`"+name+"`") {
			t.Errorf("README.md's shellLsp/documentDialect row does not name the source %q", name)
		}
	}
}

var extensionSourceList = regexp.MustCompile(`const (?:script|host)Sources = \[([^\]]*)\]`)

var quoted = regexp.MustCompile(`"([^"]+)"`)

func TestVSCodeExtensionKnowsEverySource(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "editors", "vscode", "src", "dialect.ts")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lists := extensionSourceList.FindAllStringSubmatch(string(raw), -1)
	if len(lists) != 2 {
		t.Fatalf("%s declares %d of its scriptSources and hostSources lists, want both", path, len(lists))
	}
	var got []string
	for _, list := range lists {
		for _, name := range quoted.FindAllStringSubmatch(list[1], -1) {
			got = append(got, name[1])
		}
	}
	if want := slices.Sorted(slices.Values(sourceNames[:])); !slices.Equal(slices.Sorted(slices.Values(got)), want) {
		t.Errorf("%s lists the sources %v, the server reports %v", path, got, want)
	}
}
