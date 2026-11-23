package bake

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestJSONReadsTargets(t *testing.T) {
	t.Parallel()

	got, err := parseJSON(`{
  "variable": {"TAG": {"default": "1"}},
  "group": {"default": {"targets": ["b", "a"]}},
  "target": {
    "b": {"context": "b", "contexts": {"x": "docker-image://x", "y": "${Y}", "z": "$${z}"}},
    "a": {"inherits": ["b"], "dockerfile": "${FILE}", "tags": ["a:1"]}
  }
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := []target{
		{Name: "a", Dockerfile: &Value{}, Inherits: []string{"b"}},
		{Name: "b", Context: known("b"), Contexts: map[string]Value{
			"x": {Text: "docker-image://x", Known: true},
			"y": {},
			"z": {Text: "${z}", Known: true},
		}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("targets (-want +got):\n%s", diff)
	}
	if _, err := parseJSON(`{"target": {"a": {"context": 1}}}`); err == nil {
		t.Error("a context that is no string is accepted")
	}
}

func TestComposeReadsAdditionalContexts(t *testing.T) {
	t.Parallel()

	got, err := parseCompose(`services:
  web:
    build:
      context: ./web
      dockerfile: web.Dockerfile
      additional_contexts:
        base: docker-image://base
        cost: "$$5"
        env: ${ENV}
  api:
    build:
      additional_contexts:
        - deps=target:deps
        - broken
  db:
    image: postgres:16
  plain:
    build: ./plain
`)
	if err != nil {
		t.Fatal(err)
	}
	want := []target{
		{Name: "web", Context: known("./web"), Dockerfile: known("web.Dockerfile"), Contexts: map[string]Value{
			"base": {Text: "docker-image://base", Known: true},
			"cost": {Text: "$5", Known: true},
			"env":  {},
		}},
		{Name: "api", Contexts: map[string]Value{"deps": {Text: "target:deps", Known: true}}},
		{Name: "plain", Context: known("./plain")},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("targets (-want +got):\n%s", diff)
	}
}

func TestParseClassifiesUnreadableFiles(t *testing.T) {
	t.Parallel()

	broken := "x = \"open\n"
	if targets, err := parse("/p/terraform.hcl", []byte(broken)); err != nil || targets != nil {
		t.Errorf("an unreadable .hcl that is no bake file = %v, %v; want it skipped", targets, err)
	}
	if _, err := parse("/p/docker-bake.hcl", []byte(broken)); err == nil || !strings.Contains(err.Error(), "/p/docker-bake.hcl: line 1, column 5") {
		t.Errorf("an unreadable docker-bake.hcl = %v, want an error naming its position", err)
	}
	anchors := "x-base: &base\n  image: a\nservices:\n  web:\n    <<: *base\n"
	if targets, err := parse("/p/compose.yaml", []byte(anchors)); err != nil || targets != nil {
		t.Errorf("a compose file without additional_contexts = %v, %v; want it skipped", targets, err)
	}
	if _, err := parse("/p/compose.yaml", []byte(anchors+"    build:\n      additional_contexts: {a: b}\n")); err == nil {
		t.Error("an unreadable compose file with additional_contexts is skipped")
	}
}

func TestBuildsResolveDockerfilesAndInheritance(t *testing.T) {
	t.Parallel()

	targets := []target{
		{Name: "base", Contexts: map[string]Value{"a": {Text: "docker-image://a:1", Known: true}, "b": {Text: "./b", Known: true}}},
		{Name: "app", Context: known("app"), Inherits: []string{"base"}, Contexts: map[string]Value{"a": {Text: "target:a", Known: true}}},
		{Name: "abs", Context: known("ctx"), Dockerfile: known("/elsewhere/Dockerfile"), Contexts: map[string]Value{"c": {}}},
		{Name: "remote", Context: known("https://github.com/x/y.git"), Contexts: map[string]Value{"d": {}}},
		{Name: "unknown", Context: &Value{}, Contexts: map[string]Value{"e": {}}},
		{Name: "loop", Inherits: []string{"loop2"}, Context: known("loop")},
		{Name: "loop2", Inherits: []string{"loop"}, Contexts: map[string]Value{"f": {}}},
		{Name: "app", Dockerfile: known("Other")},
	}
	got := Dockerfiles{}
	builds("/repo", targets, got)
	want := Dockerfiles{
		"/repo/Dockerfile": {
			"a": {{Text: "docker-image://a:1", Known: true}},
			"b": {{Text: "./b", Known: true}},
		},
		"/repo/app/Other": {
			"a": {{Text: "target:a", Known: true}},
			"b": {{Text: "./b", Known: true}},
		},
		"/elsewhere/Dockerfile": {"c": {{}}},
		"/repo/loop/Dockerfile": {"f": {{}}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("dockerfiles (-want +got):\n%s", diff)
	}
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, text := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestResolverFindsBakeFilesInAncestors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"build.hcl":                     "target \"t\" {\n  context = \"build\"\n  contexts = {\n    infra = \"docker-image://localhost:5000/infra:latest\"\n    deps  = \"target:deps\"\n  }\n}\n",
		"docker-bake.hcl":               "target \"o\" {\n  context = \"build\"\n  dockerfile = \"other.Dockerfile\"\n}\n",
		"docker-bake.override.hcl":      "target \"o\" {\n  contexts = { x = \"y\" }\n}\n",
		"notes.hcl":                     "this is no HCL \"\n",
		"build/Dockerfile":              "FROM infra\n",
		"build/other.Dockerfile":        "FROM x\n",
		"build/sub/compose.yaml":        "services:\n  s:\n    build:\n      context: ..\n      additional_contexts:\n        sub: ../sub\n",
		"build/sub/docker-bake.json":    "{\"target\": {\"j\": {\"context\": \".\", \"contexts\": {\"j\": \"docker-image://j\"}}}}",
		"build/sub/Dockerfile":          "FROM j\n",
		"unrelated/docker-bake.hcl":     "target \"u\" {\n  context = \"../build\"\n  contexts = { u = \"u\" }\n}\n",
		"build/sub/directory.hcl/inner": "",
	})
	reads := 0
	read := func(path string) ([]byte, error) {
		reads++
		return os.ReadFile(path)
	}
	r := NewResolver(read, os.ReadDir, func() time.Time { return time.Time{} }, time.Hour)
	got, err := r.Dockerfiles(filepath.Join(dir, "build"))
	if err != nil {
		t.Fatal(err)
	}
	want := Dockerfiles{
		filepath.Join(dir, "build", "Dockerfile"): {
			"infra": {{Text: "docker-image://localhost:5000/infra:latest", Known: true}},
			"deps":  {{Text: "target:deps", Known: true}},
		},
		filepath.Join(dir, "build", "other.Dockerfile"): {"x": {{Text: "y", Known: true}}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("dockerfiles of build (-want +got):\n%s", diff)
	}
	first := reads
	if _, err := r.Dockerfiles(filepath.Join(dir, "build")); err != nil {
		t.Fatal(err)
	}
	if reads != first {
		t.Errorf("%d more reads for files already read", reads-first)
	}
	sub, err := r.Dockerfiles(filepath.Join(dir, "build", "sub"))
	if err != nil {
		t.Fatal(err)
	}
	if want := (Contexts{"j": {{Text: "docker-image://j", Known: true}}}); !cmp.Equal(sub.Of(filepath.Join(dir, "build", "sub", "Dockerfile")), want) {
		t.Errorf("dockerfiles of build/sub = %v, want j", sub)
	}
}

func TestResolverReportsBrokenBakeFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"docker-bake.json": "{", "app/Dockerfile": "FROM a\n"})
	r := NewResolver(os.ReadFile, os.ReadDir, time.Now, time.Hour)
	if _, err := r.Dockerfiles(filepath.Join(dir, "app")); err == nil || !strings.Contains(err.Error(), "docker-bake.json") {
		t.Errorf("error = %v, want one naming docker-bake.json", err)
	}
	writeFiles(t, dir, map[string]string{"docker-bake.json": "{}"})
	r.Invalidate(filepath.Join(dir, "docker-bake.json"))
	if _, err := r.Dockerfiles(filepath.Join(dir, "app")); err != nil {
		t.Errorf("after the fix and Invalidate: %v", err)
	}
}

func TestResolverSkipsDirectoriesItCannotList(t *testing.T) {
	t.Parallel()

	failing := errors.New("disk on fire")
	list := func(dir string) ([]fs.DirEntry, error) {
		switch filepath.Base(dir) {
		case "private":
			return nil, fs.ErrPermission
		case "burning":
			return nil, failing
		}
		return nil, fs.ErrNotExist
	}
	r := NewResolver(os.ReadFile, list, time.Now, time.Hour)
	if _, err := r.Dockerfiles("/x/private/app"); err != nil {
		t.Errorf("an unlistable ancestor = %v, want it skipped", err)
	}
	if _, err := r.Dockerfiles("/burning/app"); !errors.Is(err, failing) {
		t.Errorf("a listing failure = %v, want it reported", err)
	}
}
