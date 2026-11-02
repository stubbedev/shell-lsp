package hostnav

import (
	"context"
	"path/filepath"
	"runtime"
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

func TestProjectsAreDroppedWhenTheirLastFileCloses(t *testing.T) {
	t.Parallel()

	tr, env := flakeDir(t, map[string]string{"flake.nix": sharedFlake, "a.nix": "{ }\n", "b.nix": "{ }\n"})
	linked(t, tr.host(t, "flake.nix"), env)
	linked(t, tr.host(t, "a.nix"), env)
	env.Projects.Forget(filepath.Join(tr.dir, "flake.nix"))
	if got := len(env.Projects.linked); got != 1 {
		t.Errorf("after closing one of two files, %d projects are held", got)
	}
	env.Projects.Forget(filepath.Join(tr.dir, "a.nix"))
	if got := len(env.Projects.linked); got != 0 {
		t.Errorf("after closing every file, %d projects are held", got)
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
