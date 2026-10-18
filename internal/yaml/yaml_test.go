package yaml

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/stubbedev/shell-lsp/internal/host/hosttest"
)

var update = flag.Bool("update", false, "rewrite the golden files from yq")

const (
	yqVersion = "yq (https://github.com/mikefarah/yq/) version v4.53.3"
	rejected  = "rejected\n"
)

var (
	coreInt   = regexp.MustCompile(`^([-+]?[0-9]+|0o[0-7]+|0x[0-9a-fA-F]+)$`)
	coreFloat = regexp.MustCompile(`^([-+]?(\.[0-9]+|[0-9]+(\.[0-9]*)?)([eE][-+]?[0-9]+)?|[-+]?\.(inf|Inf|INF)|\.(nan|NaN|NAN))$`)
)

func tagged(n *Node) any {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case Sequence:
		out := []any{}
		for _, item := range n.Items {
			out = append(out, tagged(item))
		}
		return out
	case Mapping:
		out := map[string]any{}
		for _, p := range n.Pairs {
			out[p.Key.Value] = tagged(p.Value)
		}
		return out
	case Scalar:
	}
	tag := "!!str"
	_, isBool := n.Bool()
	switch {
	case n.Quoted:
	case n.Null():
		tag = "!!null"
	case isBool:
		tag = "!!bool"
	case coreInt.MatchString(n.Value):
		tag = "!!int"
	case coreFloat.MatchString(n.Value):
		tag = "!!float"
	}
	return tag + " " + n.Value
}

func yq(t *testing.T, path string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "yq", "-o=json", "-I=0", `(.. | select(kind == "scalar")) |= (tag + " " + (. | to_string))`, path)
	out, err := cmd.Output()
	if errors.As(err, new(*exec.ExitError)) {
		return rejected
	}
	if err != nil {
		t.Fatalf("running yq on %s: %v", path, err)
	}
	return string(out)
}

func golden(t *testing.T, path string) string {
	t.Helper()
	goldenPath := strings.TrimSuffix(path, ".yaml") + ".golden"
	if *update {
		if err := os.WriteFile(goldenPath, []byte(yq(t, path)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("%v; record it with go test ./internal/yaml -update", err)
	}
	return string(raw)
}

func fixtures(t *testing.T, dir string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", dir, "*.yaml"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no fixtures in testdata/%s: %v", dir, err)
	}
	return paths
}

func parseFile(t *testing.T, path string) (*Node, error) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return Parse(string(raw))
}

func TestParseAgreesWithYq(t *testing.T) {
	t.Parallel()

	if *update {
		hosttest.RequireTool(t, "yq", yqVersion)
	}
	for _, path := range append(fixtures(t, "valid"), fixtures(t, "invalid")...) {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			want := golden(t, path)
			got, err := parseFile(t, path)
			if want == rejected {
				if err == nil {
					t.Errorf("yq rejects %s, but Parse accepts it as %v", path, tagged(got))
				}
				return
			}
			if err != nil {
				t.Fatalf("yq accepts %s, but Parse fails: %v", path, err)
			}
			var expected any
			if err := json.Unmarshal([]byte(want), &expected); err != nil {
				t.Fatalf("reading the yq golden for %s: %v", path, err)
			}
			if expected == "!!null " {
				expected = nil
			}
			if diff := cmp.Diff(expected, tagged(got)); diff != "" {
				t.Errorf("Parse(%s) differs from yq (-yq +ours):\n%s", path, diff)
			}
		})
	}
}

func TestParseRejectsMoreThanYq(t *testing.T) {
	t.Parallel()

	if *update {
		hosttest.RequireTool(t, "yq", yqVersion)
	}
	for _, path := range fixtures(t, "rejected") {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			if golden(t, path) == rejected {
				t.Fatalf("yq rejects %s too; move it to testdata/invalid", path)
			}
			_, err := parseFile(t, path)
			if yamlErr := new(Error); !errors.As(err, &yamlErr) {
				t.Errorf("Parse(%s) = %v, want a positioned *Error", path, err)
			}
		})
	}
}

func TestParseReportsPositions(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		text         string
		line, column int
	}{
		{"a: 1\na: 2\n", 2, 1},
		{"a:\n  b: [1, 2\n", 3, 1},
		{"a: &x 1\n", 1, 4},
		{"a:\n\t- b\n", 2, 1},
		{"x:\n  é: \"\\q\"\n", 2, 7},
	} {
		_, err := Parse(tt.text)
		var yamlErr *Error
		if !errors.As(err, &yamlErr) || yamlErr.Line != tt.line || yamlErr.Column != tt.column {
			t.Errorf("Parse(%q) = %v, want an error at %d:%d", tt.text, err, tt.line, tt.column)
		}
	}
	root, err := Parse("a:\n  - b\n")
	if err != nil {
		t.Fatal(err)
	}
	value, _ := root.Get("a")
	if item := value.Items[0]; item.Line != 2 || item.Column != 5 {
		t.Errorf("the item b sits at %d:%d, want 2:5", item.Line, item.Column)
	}
}
