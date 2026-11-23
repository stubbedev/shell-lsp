package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/gitrepo"
)

func TestTheServerDiscoversRepositoriesWithinTheCeilingWhateverTheGitDirectoryVariablesSay(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, dir := range []string{"repo/.git/objects", "repo/.git/refs", "repo/sub/deep"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "repo", ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lookup := func(vars map[string]string, dir string) bool {
		t.Helper()
		p := Process{WorkDir: root, Git: gitrepo.Process{Variables: vars}}
		ignores := gitrepo.NewIgnores(p.git(), os.ReadFile, time.Now, time.Hour)
		repo, found, err := ignores.Repository(filepath.Join(root, filepath.FromSlash(dir)))
		if err != nil {
			t.Fatalf("Repository(%s) with %v: %v", dir, vars, err)
		}
		return found && repo.Top == filepath.Join(root, "repo")
	}
	location := map[string]string{"GIT_DIR": "elsewhere", "GIT_WORK_TREE": "", "GIT_COMMON_DIR": "x"}
	if !lookup(location, "repo/sub") {
		t.Error("the server followed the git directory variables instead of discovering the repository")
	}
	location["GIT_CEILING_DIRECTORIES"] = filepath.Join(root, "repo", "sub")
	if lookup(location, "repo/sub/deep") {
		t.Error("the server searched past GIT_CEILING_DIRECTORIES")
	}
}
