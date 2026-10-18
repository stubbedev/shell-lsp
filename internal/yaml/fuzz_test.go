package yaml

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func checkPositions(t *testing.T, text string, n *Node) {
	t.Helper()
	if n == nil {
		return
	}
	lines := strings.Count(text, "\n") + 1
	if n.Line < 1 || n.Line > lines || n.Column < 1 {
		t.Fatalf("node %+v of %q sits outside the text", n, text)
	}
	for _, item := range n.Items {
		checkPositions(t, text, item)
	}
	for _, p := range n.Pairs {
		checkPositions(t, text, p.Key)
		checkPositions(t, text, p.Value)
	}
}

func FuzzParse(f *testing.F) {
	paths, _ := filepath.Glob(filepath.Join("testdata", "*", "*.yaml"))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(string(raw))
	}
	f.Fuzz(func(t *testing.T, text string) {
		root, err := Parse(text)
		if err != nil {
			var yamlErr *Error
			if !errors.As(err, &yamlErr) || yamlErr.Line < 1 || yamlErr.Line > strings.Count(text, "\n")+1 || yamlErr.Column < 1 {
				t.Fatalf("Parse(%q) fails with %v, which is not positioned within the text", text, err)
			}
			return
		}
		checkPositions(t, text, root)
	})
}
