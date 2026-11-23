package yaml

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
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
		{"a: 1\nb:\n  - *missing\n", 3, 5},
		{"a: &x [*x]\n", 1, 8},
		{"a: &x &y 1\n", 1, 7},
		{"a: &a 1\n<<: *a\n", 2, 1},
		{"a: &x 1\nb:\n  <<: [*x]\n", 3, 3},
		{"a:\n\t- b\n", 2, 1},
		{"x:\n  é: \"\\q\"\n", 2, 7},
		{"a: !foo 1\n", 1, 4},
		{"a: !!str 1\n", 1, 4},
		{"a: !\n", 1, 4},
		{"a: !reset[]\n", 1, 10},
		{"- !reset x\n", 1, 3},
		{"!reset a: 1\n", 1, 1},
		{"a: &x !reset 1\n", 1, 7},
		{"a: !reset\na: 1\n", 2, 1},
		{"m: {a: !custom 1}\n", 1, 8},
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

func parseTagged(t *testing.T, text string) any {
	t.Helper()
	root, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse(%q): %v", text, err)
	}
	return tagged(root)
}

func TestParseMergesMappings(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, text string
		want       any
	}{
		{
			name: "explicit keys win over a merged mapping on either side of it",
			text: "a: &a {x: 1, y: 1, z: 1}\nm:\n  x: 2\n  <<: *a\n  z: 3\n",
			want: map[string]any{"x": "!!int 2", "y": "!!int 1", "z": "!!int 3"},
		},
		{
			name: "earlier merged mappings win over later ones",
			text: "a: &a {x: 1}\nb: &b {x: 2, y: 2}\nm:\n  <<: [*a, *b]\n",
			want: map[string]any{"x": "!!int 1", "y": "!!int 2"},
		},
		{
			name: "explicit keys win over every merged mapping",
			text: "a: &a {x: 1}\nb: &b {x: 2, y: 2}\nm: {<<: [*a, *b], y: 3}\n",
			want: map[string]any{"x": "!!int 1", "y": "!!int 3"},
		},
		{
			name: "an inline mapping merges like an alias",
			text: "m:\n  <<: {x: 1}\n  y: 2\n",
			want: map[string]any{"x": "!!int 1", "y": "!!int 2"},
		},
		{
			name: "a quoted << is an ordinary key",
			text: "m:\n  \"<<\": {x: 1}\n",
			want: map[string]any{"<<": map[string]any{"x": "!!int 1"}},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, ok := parseTagged(t, tt.text).(map[string]any)
			if !ok {
				t.Fatalf("Parse(%q) is no mapping", tt.text)
			}
			if diff := cmp.Diff(tt.want, m["m"]); diff != "" {
				t.Errorf("Parse(%q) merges wrong (-want +got):\n%s", tt.text, diff)
			}
		})
	}
}

func TestParseKeepsMergedKeysInDocumentOrder(t *testing.T) {
	t.Parallel()

	root, err := Parse("a: &a {x: 1, y: 1}\nm:\n  w: 0\n  <<: *a\n  y: 2\n  z: 3\n")
	if err != nil {
		t.Fatal(err)
	}
	m, _ := root.Get("m")
	var keys []string
	for _, p := range m.Pairs {
		keys = append(keys, p.Key.Value)
	}
	if diff := cmp.Diff([]string{"w", "x", "y", "z"}, keys); diff != "" {
		t.Errorf("merged keys out of order (-want +got):\n%s", diff)
	}
}

func TestParseRejectsAliasBombs(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	sb.WriteString("l0: &l0 [x, x, x, x, x, x, x, x, x, x]\n")
	for i := 1; i < 10; i++ {
		fmt.Fprintf(&sb, "l%d: &l%d [", i, i)
		for j := range 10 {
			if j > 0 {
				sb.WriteString(", ")
			}
			fmt.Fprintf(&sb, "*l%d", i-1)
		}
		sb.WriteString("]\n")
	}
	_, err := Parse(sb.String())
	var yamlErr *Error
	if !errors.As(err, &yamlErr) || !strings.Contains(yamlErr.Message, "alias bomb") {
		t.Errorf("Parse of a billion laughs = %v, want an alias bomb error", err)
	}
}

func TestParseComposeWithAnchors(t *testing.T) {
	t.Parallel()

	got := parseTagged(t, `x-common: &common
  restart: unless-stopped
  environment:
    TZ: UTC
  build: &build
    context: .
    additional_contexts:
      base: ../base

services:
  app:
    <<: *common
    build:
      <<: *build
      dockerfile: app/Dockerfile
      additional_contexts:
        shared: docker-image://alpine:3.20
  worker:
    <<: *common
    command: ["worker", "--queue", default]
`)
	common := map[string]any{
		"restart":     "!!str unless-stopped",
		"environment": map[string]any{"TZ": "!!str UTC"},
		"build": map[string]any{
			"context":             "!!str .",
			"additional_contexts": map[string]any{"base": "!!str ../base"},
		},
	}
	want := map[string]any{
		"x-common": common,
		"services": map[string]any{
			"app": map[string]any{
				"restart":     "!!str unless-stopped",
				"environment": map[string]any{"TZ": "!!str UTC"},
				"build": map[string]any{
					"context":             "!!str .",
					"dockerfile":          "!!str app/Dockerfile",
					"additional_contexts": map[string]any{"shared": "!!str docker-image://alpine:3.20"},
				},
			},
			"worker": map[string]any{
				"restart":     "!!str unless-stopped",
				"environment": map[string]any{"TZ": "!!str UTC"},
				"build":       common["build"],
				"command":     []any{"!!str worker", "!!str --queue", "!!str default"},
			},
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("the compose file parses wrong (-want +got):\n%s", diff)
	}
}

func TestParseAppliesComposeTags(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, text string
		want       any
	}{
		{
			name: "!reset removes a key that a merged mapping provides",
			text: "a: &a {x: 1, y: 1}\nm:\n  <<: *a\n  x: !reset null\n",
			want: map[string]any{"y": "!!int 1"},
		},
		{
			name: "!reset before the merge key removes the merged key too",
			text: "m:\n  x: !reset []\n  <<: {x: 1, y: 2}\n",
			want: map[string]any{"y": "!!int 2"},
		},
		{
			name: "!reset ignores a block value",
			text: "a: &a {x: [1]}\nm:\n  <<: *a\n  x: !reset\n    - 2\n  y: 3\n",
			want: map[string]any{"y": "!!int 3"},
		},
		{
			name: "!override replaces a merged mapping wholesale",
			text: "a: &a {x: {p: 1, q: 1}}\nm:\n  <<: *a\n  x: !override\n    p: 2\n",
			want: map[string]any{"x": map[string]any{"p": "!!int 2"}},
		},
		{
			name: "!override takes a sequence at the indentation of its key",
			text: "a: &a {x: [1, 2]}\nm:\n  <<: *a\n  x: !override\n  - 3\n",
			want: map[string]any{"x": []any{"!!int 3"}},
		},
		{
			name: "!reset removes a key of a mapping without a merge",
			text: "m:\n  x: !reset {}\n  y: 1\n",
			want: map[string]any{"y": "!!int 1"},
		},
		{
			name: "!override keeps the value of a mapping without a merge",
			text: "m:\n  x: !override 1 # one\n",
			want: map[string]any{"x": "!!int 1"},
		},
		{
			name: "a tag precedes an anchor",
			text: "m:\n  x: !override &v 1\n  y: *v\n",
			want: map[string]any{"x": "!!int 1", "y": "!!int 1"},
		},
		{
			name: "tags apply inside a flow mapping",
			text: "m: {<<: {x: 1, y: 1, z: 1}, x: !reset , y: !override 2, z: !reset}\n",
			want: map[string]any{"y": "!!int 2"},
		},
		{
			name: "!reset can leave a mapping empty",
			text: "m:\n  x: !reset\n",
			want: map[string]any{},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, ok := parseTagged(t, tt.text).(map[string]any)
			if !ok {
				t.Fatalf("Parse(%q) is no mapping", tt.text)
			}
			if diff := cmp.Diff(tt.want, m["m"]); diff != "" {
				t.Errorf("Parse(%q) applies the tags wrong (-want +got):\n%s", tt.text, diff)
			}
		})
	}
}

func TestParseComposeWithResetTags(t *testing.T) {
	t.Parallel()

	got := parseTagged(t, `x-common: &common
  restart: unless-stopped
  ports:
    - "8080:80"
  environment:
    TZ: UTC
  build: &build
    context: .
    additional_contexts:
      base: ../base

services:
  app:
    <<: *common
    ports: !reset []
    build:
      <<: *build
      additional_contexts: !override
        shared: docker-image://alpine:3.20
  worker:
    <<: *common
    environment: !reset null
    build:
      <<: *build
      dockerfile: worker/Dockerfile
`)
	build := map[string]any{
		"context":             "!!str .",
		"additional_contexts": map[string]any{"base": "!!str ../base"},
	}
	want := map[string]any{
		"x-common": map[string]any{
			"restart":     "!!str unless-stopped",
			"ports":       []any{"!!str 8080:80"},
			"environment": map[string]any{"TZ": "!!str UTC"},
			"build":       build,
		},
		"services": map[string]any{
			"app": map[string]any{
				"restart":     "!!str unless-stopped",
				"environment": map[string]any{"TZ": "!!str UTC"},
				"build": map[string]any{
					"context":             "!!str .",
					"additional_contexts": map[string]any{"shared": "!!str docker-image://alpine:3.20"},
				},
			},
			"worker": map[string]any{
				"restart": "!!str unless-stopped",
				"ports":   []any{"!!str 8080:80"},
				"build": map[string]any{
					"context":             "!!str .",
					"dockerfile":          "!!str worker/Dockerfile",
					"additional_contexts": map[string]any{"base": "!!str ../base"},
				},
			},
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("the compose file parses wrong (-want +got):\n%s", diff)
	}
}
