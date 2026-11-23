package bake

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/stubbedev/shell-lsp/internal/gitrepo"
	"github.com/stubbedev/shell-lsp/internal/platform"
)

func newTestResolver() *Resolver {
	return NewResolver(os.ReadFile, os.ReadDir, func() time.Time { return time.Time{} }, time.Hour, gitrepo.Environment{})
}

func dockerfilesOf(t *testing.T, r *Resolver, dir string) Dockerfiles {
	t.Helper()
	got, err := r.Dockerfiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Cut != nil {
		t.Errorf("dockerfiles of %s: %s", dir, got.Cut)
	}
	return got.Dockerfiles
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
	}, filepath.Join(dir, "app", "app.Dockerfile"): {"x": {{Text: "y", Known: true}}}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("dockerfiles (-want +got):\n%s", diff)
	}
}

func TestResolverResolvesDefaultsThroughOtherDefaultsUntilACycle(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"vars.hcl": "variable \"REGISTRY\" {\n  default = \"registry.example\"\n}\n",
		"docker-bake.hcl": "variable \"IMAGE\" {\n  default = \"${REPO}:1\"\n}\nvariable \"REPO\" {\n  default = \"${REGISTRY}/base\"\n}\n" +
			"variable \"PING\" {\n  default = \"${PONG}\"\n}\nvariable \"PONG\" {\n  default = \"${PING}-x\"\n}\nvariable \"SELF\" {\n  default = \"${SELF}\"\n}\n" +
			"variable \"AFTER\" {\n  default = \"${PING}\"\n}\nvariable \"MISSING\" {\n  default = \"${NOWHERE}\"\n}\n" +
			"target \"app\" {\n  context = \"app\"\n  contexts = {\n    base = \"docker-image://${IMAGE}\"\n    ping = \"${PING}\"\n    self = \"${SELF}\"\n    after = \"${AFTER}\"\n    missing = \"${MISSING}\"\n  }\n}\n",
	})
	got := dockerfilesOf(t, newTestResolver(), filepath.Join(dir, "app"))
	want := Dockerfiles{filepath.Join(dir, "app", "Dockerfile"): {
		"base":    {{Text: "docker-image://registry.example/base:1", Known: true}},
		"ping":    {{}},
		"self":    {{}},
		"after":   {{}},
		"missing": {{}},
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

func TestResolverReportsWhereTheRepositoryWalkStops(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		".git/HEAD":         "ref: refs/heads/main\n",
		"a/Dockerfile":      "FROM base\n",
		"b/docker-bake.hcl": "target \"b\" {\n  context = \"../a\"\n  contexts = { base = \"x\" }\n}\n",
	})
	complete := newTestResolver()
	complete.limit = 3
	if got := dockerfilesOf(t, complete, filepath.Join(dir, "a")); len(got) != 1 {
		t.Errorf("dockerfiles with a limit of every directory = %v, want the context from b", got)
	}

	cut := newTestResolver()
	cut.limit = 2
	got, err := cut.Dockerfiles(filepath.Join(dir, "a"))
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(&Cut{Root: dir, Limit: 2}, got.Cut); diff != "" {
		t.Errorf("cut (-want +got):\n%s", diff)
	}
	again, err := cut.Dockerfiles(filepath.Join(dir, "a"))
	if err != nil {
		t.Fatal(err)
	}
	if again.Cut == nil {
		t.Error("a cached walk forgets that it was cut")
	}
}

func TestResolverSkipsDirectoriesTheRepositoryExcludes(t *testing.T) {
	t.Parallel()

	const (
		dockerfile = "FROM base\n"
		hidden     = "target \"h\" {\n  context = \"../../build\"\n  contexts = { hidden = \"x\" }\n}\n"
		shown      = "target \"s\" {\n  context = \"../../build\"\n  contexts = { shown = \"x\" }\n}\n"
	)
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"main/.git/HEAD":                   "ref: refs/heads/main\n",
		"main/.git/info/exclude":           "/deploy/hidden/\n",
		"main/.git/worktrees/wt/commondir": "../..\n",
		"main/build/Dockerfile":            dockerfile,
		"main/deploy/hidden/a.hcl":         hidden,
		"main/deploy/shown/a.hcl":          shown,
		"wt/.git":                          "gitdir: ../main/.git/worktrees/wt\n",
		"wt/build/Dockerfile":              dockerfile,
		"wt/deploy/hidden/a.hcl":           hidden,
		"wt/deploy/shown/a.hcl":            shown,
	})
	r := newTestResolver()
	for _, repo := range []string{"main", "wt"} {
		got := dockerfilesOf(t, r, filepath.Join(dir, repo, "build"))
		want := Dockerfiles{filepath.Join(dir, repo, "build", "Dockerfile"): {"shown": {{Text: "x", Known: true}}}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("dockerfiles of %s/build (-want +got):\n%s", repo, diff)
		}
	}
}

func TestResolverSkipsDirectoriesTheUsersExcludesFileIgnores(t *testing.T) {
	t.Parallel()

	const (
		dockerfile = "FROM base\n"
		bakeFile   = "target \"%s\" {\n  context = \"../build\"\n  contexts = { %s = \"x\" }\n}\n"
	)
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"home/.config/git/ignore":        "generated/\nvendored/\n",
		"repo/.git/HEAD":                 "ref: refs/heads/main\n",
		"repo/.gitignore":                "!vendored/\n",
		"repo/build/Dockerfile":          dockerfile,
		"repo/generated/docker-bake.hcl": fmt.Sprintf(bakeFile, "g", "generated"),
		"repo/vendored/docker-bake.hcl":  fmt.Sprintf(bakeFile, "v", "vendored"),
	})
	r := NewResolver(os.ReadFile, os.ReadDir, func() time.Time { return time.Time{} }, time.Hour, gitrepo.Environment{User: platform.UserDirs{Home: filepath.Join(dir, "home")}})
	got := dockerfilesOf(t, r, filepath.Join(dir, "repo", "build"))
	want := Dockerfiles{filepath.Join(dir, "repo", "build", "Dockerfile"): {"vendored": {{Text: "x", Known: true}}}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("dockerfiles of repo/build (-want +got):\n%s", diff)
	}
}

func TestResolverInvalidateAllRereadsTheUsersExcludesFile(t *testing.T) {
	t.Parallel()

	const bakeFile = "target \"%s\" {\n  context = \"../build\"\n  contexts = { %s = \"x\" }\n}\n"
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"home/.gitconfig":                "[core]\n\texcludesFile = ~/first\n",
		"home/first":                     "generated/\n",
		"home/second":                    "vendored/\n",
		"repo/.git/HEAD":                 "ref: refs/heads/main\n",
		"repo/build/Dockerfile":          "FROM base\n",
		"repo/generated/docker-bake.hcl": fmt.Sprintf(bakeFile, "g", "generated"),
		"repo/vendored/docker-bake.hcl":  fmt.Sprintf(bakeFile, "v", "vendored"),
	})
	r := NewResolver(os.ReadFile, os.ReadDir, func() time.Time { return time.Time{} }, time.Hour, gitrepo.Environment{User: platform.UserDirs{Home: filepath.Join(dir, "home")}})
	build := filepath.Join(dir, "repo", "build")
	dockerfile := filepath.Join(build, "Dockerfile")
	vendored := Dockerfiles{dockerfile: {"vendored": {{Text: "x", Known: true}}}}
	if diff := cmp.Diff(vendored, dockerfilesOf(t, r, build)); diff != "" {
		t.Errorf("dockerfiles before the edit (-want +got):\n%s", diff)
	}
	writeFiles(t, dir, map[string]string{"home/.gitconfig": "[core]\n\texcludesFile = ~/second\n"})
	if diff := cmp.Diff(vendored, dockerfilesOf(t, r, build)); diff != "" {
		t.Errorf("dockerfiles before InvalidateAll (-want +got):\n%s", diff)
	}
	r.InvalidateAll()
	generated := Dockerfiles{dockerfile: {"generated": {{Text: "x", Known: true}}}}
	if diff := cmp.Diff(generated, dockerfilesOf(t, r, build)); diff != "" {
		t.Errorf("dockerfiles after InvalidateAll (-want +got):\n%s", diff)
	}
}
