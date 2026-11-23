package bake

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func newTestResolver() *Resolver {
	return NewResolver(os.ReadFile, os.ReadDir, func() time.Time { return time.Time{} }, time.Hour)
}

func dockerfilesOf(t *testing.T, r *Resolver, dir string) Dockerfiles {
	t.Helper()
	got, err := r.Dockerfiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestResolverFindsBakeFilesBesideTheDockerfileInTheRepository(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	writeFiles(t, dir, map[string]string{
		"repo/.git/HEAD":                      "ref: refs/heads/main\n",
		"repo/.gitignore":                     "/out/\n",
		"repo/build/Dockerfile":               "FROM base\n",
		"repo/deploy/docker-bake.hcl":         "target \"app\" {\n  context = \"../build\"\n  contexts = { base = \"docker-image://alpine\" }\n}\n",
		"repo/deploy/nested/compose.yaml":     "services:\n  s:\n    build:\n      context: ../../build\n      additional_contexts:\n        tools: target:tools\n",
		"repo/out/docker-bake.hcl":            "target \"o\" {\n  context = \"../build\"\n  contexts = { ignored = \"x\" }\n}\n",
		"repo/node_modules/x/docker-bake.hcl": "target \"n\" {\n  context = \"../../build\"\n  contexts = { heavy = \"x\" }\n}\n",
		"outside/docker-bake.hcl":             "target \"u\" {\n  context = \"../repo/build\"\n  contexts = { outside = \"x\" }\n}\n",
		"plain/build/Dockerfile":              "FROM base\n",
		"plain/deploy/docker-bake.hcl":        "target \"p\" {\n  context = \"../build\"\n  contexts = { base = \"x\" }\n}\n",
	})
	r := newTestResolver()
	want := Dockerfiles{filepath.Join(repo, "build", "Dockerfile"): {
		"base":  {{Text: "docker-image://alpine", Known: true}},
		"tools": {{Text: "target:tools", Known: true}},
	}}
	if diff := cmp.Diff(want, dockerfilesOf(t, r, filepath.Join(repo, "build"))); diff != "" {
		t.Errorf("dockerfiles of repo/build (-want +got):\n%s", diff)
	}
	if got := dockerfilesOf(t, r, filepath.Join(dir, "plain", "build")); len(got) != 0 {
		t.Errorf("dockerfiles outside any repository = %v, want none from sibling directories", got)
	}

	writeFiles(t, dir, map[string]string{"repo/later/docker-bake.json": `{"target": {"l": {"context": "../build", "contexts": {"later": "target:l"}}}}`})
	r.Invalidate(filepath.Join(repo, "later", "docker-bake.json"))
	if got := dockerfilesOf(t, r, filepath.Join(repo, "build")).Of(filepath.Join(repo, "build", "Dockerfile")); got["later"] == nil {
		t.Errorf("after a new bake file and Invalidate = %v, want the later context", got)
	}
}

func TestResolverSubstitutesVariableDefaults(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"vars.hcl": "variable \"BASE\" {\n  default = \"docker-image://alpine\"\n}\nvariable \"TOOLS\" {\n  default = \"docker-image://tools\"\n}\n",
		"docker-bake.hcl": "variable \"DIR\" {\n  default = \"app\"\n}\nvariable \"FILE\" {\n  default = \"${DIR}.Dockerfile\"\n}\nvariable \"REGISTRY\" {}\n" +
			"target \"app\" {\n  context = \"${DIR}\"\n  contexts = {\n    base = \"${BASE}\"\n    tools = \"${ TOOLS }\"\n    pinned = \"docker-image://${DIR}:1\"\n    remote = \"docker-image://${REGISTRY}/x\"\n    computed = \"${upper(DIR)}\"\n  }\n}\n" +
			"target \"file\" {\n  context = \"app\"\n  dockerfile = \"${FILE}\"\n  contexts = { x = \"y\" }\n}\n",
		"docker-bake.json": `{"variable": {"BASE": {"default": "docker-image://busybox"}}}`,
	})
	got := dockerfilesOf(t, newTestResolver(), filepath.Join(dir, "app"))
	want := Dockerfiles{filepath.Join(dir, "app", "Dockerfile"): {
		"base":     {{Text: "docker-image://busybox", Known: true}},
		"tools":    {{Text: "docker-image://tools", Known: true}},
		"pinned":   {{Text: "docker-image://app:1", Known: true}},
		"remote":   {{}},
		"computed": {{}},
	}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("dockerfiles (-want +got):\n%s", diff)
	}
}

func TestResolverFollowsInheritsAcrossTheBakeFilesOfADirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"base.hcl":        "target \"base\" {\n  contexts = { base = \"docker-image://alpine:3\" }\n}\ntarget \"app\" {\n  contexts = { other = \"x\" }\n}\n",
		"docker-bake.hcl": "target \"app\" {\n  context = \"app\"\n  inherits = [\"base\"]\n}\n",
		"sub/parent.hcl":  "target \"base\" {\n  contexts = { far = \"x\" }\n}\n",
	})
	got := dockerfilesOf(t, newTestResolver(), filepath.Join(dir, "app"))
	want := Dockerfiles{filepath.Join(dir, "app", "Dockerfile"): {"base": {{Text: "docker-image://alpine:3", Known: true}}}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("dockerfiles (-want +got):\n%s", diff)
	}
}

func TestResolverReadsCwdPathsAsLocal(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"docker-bake.hcl": "target \"app\" {\n  context = \"cwd://app\"\n  dockerfile = \"cwd://app.Dockerfile\"\n  contexts = { src = \"cwd://src\" }\n}\n",
	})
	got := dockerfilesOf(t, newTestResolver(), filepath.Join(dir, "app"))
	want := Dockerfiles{filepath.Join(dir, "app", "app.Dockerfile"): {"src": {{Text: "cwd://src", Known: true}}}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("dockerfiles (-want +got):\n%s", diff)
	}
	if _, isImage := (Value{Text: "cwd://src", Known: true}).Image(); isImage {
		t.Error("a cwd:// context value is read as an image")
	}
}
