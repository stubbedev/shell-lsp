package hostnav

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/host/nix"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

func BenchmarkNixLink(b *testing.B) {
	for _, name := range []string{"single-flake", "flake-parts", "dendritic"} {
		b.Run(name, func(b *testing.B) {
			root, err := filepath.Abs(filepath.Join("..", "..", "corpus", "nix", name))
			if err != nil {
				b.Fatal(err)
			}
			ws := newWorkspace()
			file, err := ws.Load(filepath.Join(root, "flake.nix"))
			if err != nil {
				b.Fatal(err)
			}
			h, ok := Of(file)
			if !ok {
				b.Fatalf("%s/flake.nix is not a hosted file", name)
			}
			b.ReportAllocs()
			for b.Loop() {
				g, err := Build(b.Context(), h, Env{Loader: ws})
				if err != nil {
					b.Fatal(err)
				}
				g.Module.Links()
			}
		})
	}
}

func TestNixRelinkStaysInItsMemoryBudget(t *testing.T) {
	const (
		maxRelinkBytes  = 48 << 20
		maxRelinkAllocs = 400_000
	)
	root, err := filepath.Abs(filepath.Join("..", "..", "corpus", "nix", "dendritic"))
	if err != nil {
		t.Fatal(err)
	}
	ws := newWorkspace()
	file, err := ws.Load(filepath.Join(root, "flake.nix"))
	if err != nil {
		t.Fatal(err)
	}
	h, ok := Of(file)
	if !ok {
		t.Fatal("flake.nix is not a hosted file")
	}
	projects := NewProjects()
	defer projects.Close()
	env := Env{Loader: ws, Projects: projects}
	relink := func() {
		g, err := Build(t.Context(), h, env)
		if err != nil {
			t.Fatal(err)
		}
		g.Module.Links()
		env.Stamp.Docs++
		projects.mu.Lock()
		for _, l := range projects.linked {
			l.dirty = true
		}
		projects.mu.Unlock()
	}
	relink()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	relink()
	runtime.ReadMemStats(&after)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > maxRelinkBytes {
		t.Errorf("relinking allocated %d bytes, want at most %d", allocated, maxRelinkBytes)
	}
	if allocs := testing.AllocsPerRun(1, relink); allocs > maxRelinkAllocs {
		t.Errorf("relinking made %.0f allocations, want at most %d", allocs, maxRelinkAllocs)
	}
}

func flakeDir(t *testing.T, files map[string]string) (*workspaceTree, Env) {
	t.Helper()
	tr := &workspaceTree{dir: writeTree(t, files), ws: newWorkspace()}
	projects := NewProjects()
	t.Cleanup(projects.Close)
	return tr, Env{Loader: tr.ws, Projects: projects}
}

type workspaceTree struct {
	dir string
	ws  *workspace.Workspace
}

func (tr *workspaceTree) host(t *testing.T, name string) Host {
	t.Helper()
	file, err := tr.ws.Load(filepath.Join(tr.dir, name))
	if err != nil {
		t.Fatal(err)
	}
	h, ok := Of(file)
	if !ok {
		t.Fatalf("%s is not a hosted file", name)
	}
	return h
}

func linked(t *testing.T, h Host, env Env) *Group {
	t.Helper()
	g, err := Build(t.Context(), h, env)
	if err != nil {
		t.Fatal(err)
	}
	g.Module.Links()
	return g
}

const sharedFlake = "{\n  outputs = { self }: {\n    a = import ./a.nix;\n    b = import ./b.nix;\n  };\n}\n"

func TestFilesOfOneFlakeShareOneProject(t *testing.T) {
	t.Parallel()

	tr, env := flakeDir(t, map[string]string{"flake.nix": sharedFlake, "a.nix": "{ }\n", "b.nix": "{ }\n"})
	for _, name := range []string{"flake.nix", "a.nix", "b.nix"} {
		linked(t, tr.host(t, name), env)
	}
	if got := len(env.Projects.linked); got != 1 {
		t.Errorf("three files of one flake hold %d projects, want 1", got)
	}
}

func TestAFileOutsideTheFlakeClosureGetsItsOwnProject(t *testing.T) {
	t.Parallel()

	tr, env := flakeDir(t, map[string]string{"flake.nix": sharedFlake, "a.nix": "{ }\n", "b.nix": "{ }\n", "loose.nix": "{ }\n"})
	g := linked(t, tr.host(t, "loose.nix"), env)
	if g.File(g.Index()).Path != filepath.Join(tr.dir, "loose.nix") {
		t.Errorf("the group of loose.nix serves %s", g.File(g.Index()).Path)
	}
	if got := len(env.Projects.linked); got != 2 {
		t.Errorf("holding %d projects, want the flake and the one around the loose file", got)
	}
}

func TestAnUnrelatedStampChangeKeepsAProjectThatIsStillFresh(t *testing.T) {
	t.Parallel()

	tr, env := flakeDir(t, map[string]string{"flake.nix": sharedFlake, "a.nix": "{ }\n", "b.nix": "{ }\n"})
	h := tr.host(t, "flake.nix")
	linked(t, h, env)
	first := env.Projects.linked[projectKey(nix.Request{Flake: filepath.Join(tr.dir, "flake.nix")})]
	if first == nil {
		t.Fatal("the flake has no project")
	}
	env.Stamp.Docs++
	linked(t, h, env)
	if got := env.Projects.linked[first.key]; got != first {
		t.Error("a stamp that changed nothing the project read relinked it")
	}
}

func TestProjectsOutliveTheirLastFileUntilDropped(t *testing.T) {
	t.Parallel()

	tr, env := flakeDir(t, map[string]string{"flake.nix": sharedFlake, "a.nix": "{ }\n", "b.nix": "{ }\n"})
	linked(t, tr.host(t, "flake.nix"), env)
	linked(t, tr.host(t, "a.nix"), env)
	env.Projects.Forget(filepath.Join(tr.dir, "flake.nix"))
	if got := len(env.Projects.linked); got != 1 {
		t.Errorf("after closing one of two files, %d projects are held", got)
	}
	kept := slices.Collect(maps.Values(env.Projects.linked))
	env.Projects.Forget(filepath.Join(tr.dir, "a.nix"))
	if got := len(env.Projects.linked); got != 1 {
		t.Errorf("right after closing every file, %d projects are held, want the last one kept for a reopen", got)
	}
	linked(t, tr.host(t, "a.nix"), env)
	if got := slices.Collect(maps.Values(env.Projects.linked)); len(got) != 1 || got[0] != kept[0] {
		t.Errorf("reopening a file linked its project again, %d held", len(got))
	}
	env.Projects.Forget(filepath.Join(tr.dir, "a.nix"))
	env.Projects.ReleaseIdle()
	if got := len(env.Projects.linked); got != 0 {
		t.Errorf("after closing every file and resting, %d projects are held", got)
	}
}

func TestAProjectWithNoPatienceAnswersFromTheFileAlone(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	tr, env := flakeDir(t, map[string]string{"flake.nix": sharedFlake, "a.nix": "{ }\n", "b.nix": "{ }\n"})
	env.Graph = func(context.Context) (*Graph, error) {
		<-release
		return nil, nil
	}
	env.Patience = time.Millisecond
	g := linked(t, tr.host(t, "flake.nix"), env)
	close(release)
	if len(g.Module.Files) != 1 {
		t.Errorf("while the link runs, the group holds %d files, want the open one alone", len(g.Module.Files))
	}
}

func TestAnAnswerThatDidNotWaitForTheLinkIsMarked(t *testing.T) {
	t.Parallel()

	dir := writeTree(t, map[string]string{"flake.nix": "{ outputs = _: { }; }\n", "a.nix": "{ a = 1; }\n"})
	ws := newWorkspace()
	file, err := ws.Load(filepath.Join(dir, "a.nix"))
	if err != nil {
		t.Fatal(err)
	}
	projects := NewProjects()
	t.Cleanup(projects.Close)
	called := make(chan struct{}, 1)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	graph := func(context.Context) (*Graph, error) {
		<-release
		return nil, nil
	}
	env := Env{Loader: ws, Projects: projects, Graph: graph, Patience: time.Nanosecond, OnPartial: func() { called <- struct{}{} }}
	g, err := Build(context.Background(), hostOf(t, file), env)
	if err != nil {
		t.Fatal(err)
	}
	g.Module.Links()
	if !g.Linking() {
		t.Error("a group answered before its link landed does not say so")
	}
	select {
	case <-called:
	default:
		t.Error("nothing was told that an answer did not wait for the link")
	}
}

func TestLinkedProjectsAreBoundedByTheirBytes(t *testing.T) {
	t.Parallel()

	large := "# " + strings.Repeat("x", 7<<19) + "\n{ }\n"
	tr, env := flakeDir(t, map[string]string{"a.nix": "[ ./a1.nix ./a2.nix ]\n", "a1.nix": large, "a2.nix": large, "b.nix": "[ ./b1.nix ./b2.nix ]\n", "b1.nix": large, "b2.nix": large, "c.nix": "[ ./c1.nix ./c2.nix ]\n", "c1.nix": large, "c2.nix": large})
	for _, name := range []string{"a.nix", "b.nix", "c.nix"} {
		linked(t, tr.host(t, name), env)
	}
	env.Projects.mu.Lock()
	defer env.Projects.mu.Unlock()
	if got, limit := env.Projects.linkedBytes(), linkedProjectsOfBytes*nix.MaxProjectBytes(); got > limit {
		t.Errorf("%d bytes of projects stay linked, the budget is %d", got, limit)
	}
	if len(env.Projects.linked) != 2 {
		t.Errorf("%d projects stay linked, want the two newest that fit", len(env.Projects.linked))
	}
	if env.Projects.byPath[filepath.Join(tr.dir, "c.nix")] == nil {
		t.Error("the newest project was evicted")
	}
}

func TestRetainDropsProjectsOnlyClosedFilesReached(t *testing.T) {
	t.Parallel()

	tr, env := flakeDir(t, map[string]string{"a.nix": "{ }\n", "b.nix": "{ }\n"})
	linked(t, tr.host(t, "a.nix"), env)
	linked(t, tr.host(t, "b.nix"), env)
	open := filepath.Join(tr.dir, "a.nix")
	env.Projects.Retain(func(path string) bool { return path == open })
	env.Projects.mu.Lock()
	defer env.Projects.mu.Unlock()
	if len(env.Projects.byPath) != 1 || env.Projects.byPath[open] == nil {
		t.Errorf("after retaining one file, %d files keep a project", len(env.Projects.byPath))
	}
	if len(env.Projects.linked) != 1 {
		t.Errorf("after retaining one file, %d projects are held", len(env.Projects.linked))
	}
}

func overflowingFiles(sizes ...int) map[string]string {
	files := map[string]string{}
	var imports strings.Builder
	for i, size := range sizes {
		name := fmt.Sprintf("part%d.nix", i)
		files[name] = "# " + strings.Repeat("x", size) + "\n{ }\n"
		fmt.Fprintf(&imports, "  p%d = import ./%s;\n", i, name)
	}
	files["root.nix"] = "{\n" + imports.String() + "}\n"
	return files
}

func TestALinkThatCannotFitLoadsNothing(t *testing.T) {
	t.Parallel()

	third := int(nix.MaxProjectBytes()) / 3
	tr, env := flakeDir(t, overflowingFiles(third, third, third, third, third))
	root := filepath.Join(tr.dir, "root.nix")
	l := linkProject(t.Context(), env, nix.Request{Roots: []string{root}})
	if !l.project.Truncated() {
		t.Error("a closure past the limit was not truncated")
	}
	if got := l.project.Bytes(); got != 0 {
		t.Errorf("a closure known to overflow still loaded %d bytes", got)
	}
}

func TestAShallowLinkIsNotJudgedByItsClosure(t *testing.T) {
	t.Parallel()

	third := int(nix.MaxProjectBytes()) / 3
	tr, env := flakeDir(t, overflowingFiles(third, third, third, third, third))
	root := filepath.Join(tr.dir, "root.nix")
	l := linkProject(t.Context(), env, nix.Request{Roots: []string{root}, Shallow: true})
	if l.project.Truncated() {
		t.Error("a shallow link was truncated by files it never follows")
	}
	if got := l.project.Bytes(); got == 0 {
		t.Error("a shallow link did not load its root")
	}
}

func TestALinkOverTheLimitOnlyByItsLastFileStillLinks(t *testing.T) {
	t.Parallel()

	half := int(nix.MaxProjectBytes()) / 2
	tr, env := flakeDir(t, overflowingFiles(half+half/2, half))
	root := filepath.Join(tr.dir, "root.nix")
	l := linkProject(t.Context(), env, nix.Request{Roots: []string{root}})
	if got := l.project.Bytes(); got == 0 {
		t.Errorf("a closure that may fit was refused without loading: truncated %v", l.project.Truncated())
	}
}
