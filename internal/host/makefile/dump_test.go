package makefile

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/stubbedev/shell-lsp/internal/host/hosttest"
)

var update = flag.Bool("update", false, "rewrite the golden files from the make reference binary")

type database struct {
	Variables []string `json:"variables"`
	Recipes   []string `json:"recipes"`
}

var (
	definedAt  = regexp.MustCompile(`^# (?:makefile|'override' directive) \(from '[^']+', line \d+\)$`)
	recipeAt   = regexp.MustCompile(`^#  recipe to execute \(from '[^']+', line (\d+)\):$`)
	targetLine = regexp.MustCompile(`^([^#\s][^:]*?)::?(?:\s|$)`)
	assignment = regexp.MustCompile(`^(\S+?)\s*(?:::?=|:::=|\+=|\?=|!=|=)`)
	scoped     = regexp.MustCompile(`^(\S+): (\S+?)\s*(?:::?=|\+=|\?=|!=|=)`)
	defined    = regexp.MustCompile(`^define (\S+)$`)
)

func makeDatabase(t *testing.T, path string) database {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "make", "-pqrR", "-f", filepath.Base(path))
	cmd.Dir = filepath.Dir(path)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	var out bytes.Buffer
	cmd.Stdout = &out
	_ = cmd.Run()
	db := database{Variables: []string{}, Recipes: []string{}}
	var lines []string
	sc := bufio.NewScanner(&out)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	for i, line := range lines {
		switch {
		case definedAt.MatchString(line) && i+1 < len(lines):
			next := lines[i+1]
			if m := scoped.FindStringSubmatch(next); m != nil {
				db.Variables = append(db.Variables, m[1]+":"+m[2])
			} else if m := defined.FindStringSubmatch(next); m != nil {
				db.Variables = append(db.Variables, m[1])
			} else if m := assignment.FindStringSubmatch(next); m != nil && m[1] != "MAKEFILE_LIST" {
				db.Variables = append(db.Variables, m[1])
			}
		case recipeAt.MatchString(line):
			for j := i - 1; j >= 0; j-- {
				if m := targetLine.FindStringSubmatch(lines[j]); m != nil {
					db.Recipes = append(db.Recipes, m[1]+"@"+recipeAt.FindStringSubmatch(line)[1])
					break
				}
			}
		}
	}
	db.Recipes = withoutDerived(db.Recipes)
	slices.Sort(db.Variables)
	slices.Sort(db.Recipes)
	db.Variables = slices.Compact(db.Variables)
	return db
}

func withoutDerived(recipes []string) []string {
	patternLines := map[string]bool{}
	for _, r := range recipes {
		if name, line, _ := strings.Cut(r, "@"); strings.Contains(name, "%") {
			patternLines[line] = true
		}
	}
	return slices.DeleteFunc(recipes, func(r string) bool {
		name, line, _ := strings.Cut(r, "@")
		return !strings.Contains(name, "%") && patternLines[line]
	})
}

func ours(f *File) database {
	db := database{Variables: []string{}, Recipes: []string{}}
	for _, a := range f.Assignments {
		if len(a.Targets) == 0 {
			db.Variables = append(db.Variables, a.Name.Text)
		}
		for _, target := range a.Targets {
			db.Variables = append(db.Variables, target.Text+":"+a.Name.Text)
		}
	}
	last := map[string]string{}
	var order []string
	for _, r := range f.Rules {
		if len(r.Recipe) == 0 {
			continue
		}
		line := f.line(r.Recipe[0].Span.Start)
		for _, target := range r.Targets {
			entry := fmt.Sprintf("%s@%d", target.Text, line)
			if r.DoubleColon {
				db.Recipes = append(db.Recipes, entry)
				continue
			}
			if _, seen := last[target.Text]; !seen {
				order = append(order, target.Text)
			}
			last[target.Text] = entry
		}
	}
	for _, name := range order {
		db.Recipes = append(db.Recipes, last[name])
	}
	slices.Sort(db.Variables)
	slices.Sort(db.Recipes)
	db.Variables = slices.Compact(db.Variables)
	return db
}

func TestParseAgreesWithMakeDatabase(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob(filepath.Join("testdata", "parse", "*.mk"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no parse fixtures: %v", err)
	}
	if *update {
		hosttest.RequireTool(t, "make", hosttest.MakeVersion)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			golden := strings.TrimSuffix(path, ".mk") + ".json"
			if *update {
				encoded, err := json.MarshalIndent(makeDatabase(t, path), "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(golden, append(encoded, '\n'), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			raw, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v; record it with go test -run TestParseAgreesWithMakeDatabase -update", err)
			}
			var want database
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatal(err)
			}
			text, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			f := Parse(string(text))
			if len(f.Errors) > 0 {
				t.Fatalf("parse errors in a Makefile make accepts: %+v", f.Errors)
			}
			if diff := cmp.Diff(want, ours(f)); diff != "" {
				t.Errorf("%s differs from make's database (-make +ours):\n%s", path, diff)
			}
		})
	}
}
