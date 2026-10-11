package parser

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const commentDenseScript = `#!/bin/bash
# leading
a=1 # after assignment
# before if
if true; then # after then
  : # inside
  # last in then
else
  # in else
  :
fi # after fi
case $x in
  # before item
  a) echo a ;; # after item
  # between
  b) echo b ;;
  # end of case
esac
arr=(
  # first
  one # trailing one
  two
  # last
)
f() { # in function
  echo ${x:-$(echo # in subst
  )} # after
}
`

func walkScripts(t *testing.T) map[string]string {
	t.Helper()
	scripts := map[string]string{"comment-dense": commentDenseScript}
	corpus, err := filepath.Glob(filepath.Join("testdata", "corpus", "*", "*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range corpus {
		if filepath.Ext(path) == ".golden" {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		scripts[path] = string(raw)
	}
	err = filepath.WalkDir(filepath.Join("..", "lint", "testdata", "fixtures"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, section := range strings.Split(string(raw), "\n### ")[1:] {
			var script strings.Builder
			for line := range strings.SplitSeq(section, "\n") {
				if !strings.HasPrefix(line, ">>") {
					script.WriteString(line + "\n")
				}
			}
			_, body, _ := strings.Cut(script.String(), "\n")
			scripts[fmt.Sprintf("%s#%d", path, i)] = body
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return scripts
}

func walkEvents(walk func(syntax.Node, func(syntax.Node) bool), root syntax.Node, pruneEvery int) []string {
	var events []string
	seen := 0
	walk(root, func(n syntax.Node) bool {
		if n == nil {
			events = append(events, "exit")
			return true
		}
		seen++
		events = append(events, fmt.Sprintf("%T %s-%s", n, n.Pos(), n.End()))
		return pruneEvery == 0 || seen%pruneEvery != 0
	})
	return events
}

func TestWalkMatchesSyntaxWalk(t *testing.T) {
	t.Parallel()

	scripts := walkScripts(t)
	if len(scripts) < 1000 {
		t.Fatalf("only %d scripts found; the fixture layout changed", len(scripts))
	}
	for name, text := range scripts {
		for _, d := range []dialect.Dialect{dialect.Bash, dialect.Zsh, dialect.Ksh} {
			tree := Parse(text, d)
			for _, prune := range []int{0, 2, 7} {
				want := walkEvents(syntax.Walk, tree.File, prune)
				got := walkEvents(Walk, tree.File, prune)
				if !slices.Equal(got, want) {
					t.Fatalf("%s (%s, prune every %d): walk differs from syntax.Walk\n got %v\nwant %v", name, d, prune, got, want)
				}
			}
		}
	}
}

func TestWalkVisitsCommentsInPlace(t *testing.T) {
	t.Parallel()

	tree := Parse(commentDenseScript, dialect.Bash)
	var visited []*syntax.Comment
	Walk(tree.File, func(n syntax.Node) bool {
		if c, ok := n.(*syntax.Comment); ok {
			visited = append(visited, c)
		}
		return true
	})
	again := 0
	Walk(tree.File, func(n syntax.Node) bool {
		if c, ok := n.(*syntax.Comment); ok {
			if c != visited[again] {
				t.Errorf("comment %q is a different node on the second walk", c.Text)
			}
			again++
		}
		return true
	})
	if len(visited) < 15 || again != len(visited) {
		t.Errorf("visited %d comments, then %d", len(visited), again)
	}
}

func BenchmarkWalk(b *testing.B) {
	tree := Parse(strings.Repeat(commentDenseScript, 200), dialect.Bash)
	visit := func(syntax.Node) bool { return true }
	b.Run("parser", func(b *testing.B) {
		for b.Loop() {
			Walk(tree.File, visit)
		}
	})
	b.Run("syntax", func(b *testing.B) {
		for b.Loop() {
			syntax.Walk(tree.File, visit)
		}
	})
}

func TestParseEditBuildsTheSameIndexAsParse(t *testing.T) {
	t.Parallel()

	for name, text := range walkScripts(t) {
		unrelated := Parse(text[:len(text)/2], dialect.Bash)
		want := reflect.ValueOf(Parse(text, dialect.Bash).Index)
		got := reflect.ValueOf(ParseEdit(unrelated, text, dialect.Bash, Variant(dialect.Bash, dialect.Input{Text: text})).Index)
		for i := range want.NumField() {
			field := want.Type().Field(i)
			if !field.IsExported() || field.Type.Kind() != reflect.Slice {
				continue
			}
			w, g := want.Field(i), got.Field(i)
			if w.Len() != g.Len() {
				t.Fatalf("%s: %s has %d entries, want %d", name, field.Name, g.Len(), w.Len())
			}
			for j := range w.Len() {
				if !reflect.DeepEqual(g.Index(j).Interface(), w.Index(j).Interface()) {
					t.Fatalf("%s: %s[%d] = %+v, want %+v", name, field.Name, j, g.Index(j), w.Index(j))
				}
			}
		}
	}
}
