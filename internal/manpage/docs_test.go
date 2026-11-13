//go:build !windows

package manpage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShellDocNamesTheDefaultManDirectories(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "shell.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(strings.Fields(string(raw)), " ")
	_, sentence, found := strings.Cut(text, "through `MANPATH` (")
	sentence, _, ended := strings.Cut(sentence, ")")
	if !found || !ended {
		t.Fatal("docs/shell.md no longer says where manual pages are looked up")
	}
	for _, dir := range defaultManDirs {
		if !strings.Contains(sentence, "`"+dir+"`") {
			t.Errorf("docs/shell.md does not name the default manual page directory %s", dir)
		}
	}
	if got, want := strings.Count(sentence, "`/"), len(defaultManDirs); got != want {
		t.Errorf("docs/shell.md names %d absolute manual page directories, the server searches %d", got, want)
	}
}
