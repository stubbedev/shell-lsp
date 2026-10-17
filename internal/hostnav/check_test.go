package hostnav

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/host/hosttest"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

var update = flag.Bool("update", false, "record the reference tools' verdicts and the current findings in the golden files")

func newWorkspace() *workspace.Workspace {
	return workspace.New(func(string) (*workspace.File, bool) { return nil, false }, workspace.Detect)
}

func groupFor(t *testing.T, ws *workspace.Workspace, path string, files navigation.WorkspaceFiles) *Group {
	t.Helper()
	file, err := ws.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	g, err := Build(context.Background(), hostOf(t, file), envWith(ws, hostOf(t, file).Language(), files))
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func envWith(loader Loader, language host.Language, files navigation.WorkspaceFiles) Env {
	env := Env{Loader: loader}
	if files != nil {
		env.Graph = func(ctx context.Context) (*Graph, error) { return NewGraph(ctx, language, files, env) }
	}
	return env
}

func hostOf(t *testing.T, file *workspace.File) Host {
	t.Helper()
	h, ok := Of(file)
	if !ok {
		t.Fatalf("%s is not a justfile or Makefile", file.Path)
	}
	return h
}

func writeTree(t testing.TB, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func findingsAlone(t *testing.T, name string) hosttest.Analyze {
	return func(text string) []lint.Finding {
		dir := writeTree(t, map[string]string{name: text})
		return groupFor(t, newWorkspace(), filepath.Join(dir, name), nil).Findings()
	}
}

func TestJustChecksAgreeWithJust(t *testing.T) {
	t.Parallel()

	if *update {
		hosttest.RequireTool(t, "just", hosttest.JustVersion)
	}
	hosttest.Check(t, filepath.Join("testdata", "check", "just.txt"), findingsAlone(t, "justfile"), hosttest.JustVerdict, *update)
}

func TestMakeChecksAgreeWithMake(t *testing.T) {
	t.Parallel()

	if *update {
		hosttest.RequireTool(t, "make", hosttest.MakeVersion)
	}
	hosttest.Check(t, filepath.Join("testdata", "check", "make.txt"), findingsAlone(t, "Makefile"), hosttest.MakeVerdict, *update)
}
