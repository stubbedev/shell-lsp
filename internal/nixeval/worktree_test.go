//go:build unix

package nixeval

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

func gitIn(t *testing.T, dir string) func(args ...string) {
	t.Helper()
	return func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.org",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.org",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %q: %v\n%s", args, err, out)
		}
	}
}

func treeKey(t *testing.T, dir string) string {
	t.Helper()
	digest, err := fingerprintTree(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func gitRepository(t *testing.T) (string, func(args ...string)) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub", "modules"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "sub", "flake.nix"), "{ }\n")
	writeFile(t, filepath.Join(root, "sub", "modules", "a.nix"), "{ }\n")
	writeFile(t, filepath.Join(root, "outside.nix"), "{ }\n")
	git := gitIn(t, root)
	git("init", "-q")
	git("add", ".")
	git("commit", "-q", "-m", "first")
	return root, git
}

func TestTreeKeyFollowsWhatNixSeesInAGitRepository(t *testing.T) {
	t.Parallel()
	requireGit(t)

	root, git := gitRepository(t)
	dir := filepath.Join(root, "sub")
	key := treeKey(t, dir)
	if treeKey(t, dir) != key {
		t.Fatal("the key is not stable")
	}

	untracked := filepath.Join(dir, "scratch.nix")
	writeFile(t, untracked, "{ }\n")
	if treeKey(t, dir) != key {
		t.Error("an untracked file changed the key, and nix cannot see it")
	}
	ignoredDir := filepath.Join(root, "node_modules")
	if err := os.Mkdir(ignoredDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(ignoredDir, "x.js"), "1\n")
	if treeKey(t, dir) != key {
		t.Error("an untracked directory changed the key")
	}

	module := filepath.Join(dir, "modules", "a.nix")
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(module, future, future); err != nil {
		t.Fatal(err)
	}
	if treeKey(t, dir) != key {
		t.Error("touching a file changed the key, and its content is the same")
	}

	writeFile(t, module, "{ a = 1; }\n")
	edited := treeKey(t, dir)
	if edited == key {
		t.Error("an edit of a tracked module left the key alone")
	}
	writeFile(t, module, "{ }\n")
	if treeKey(t, dir) != key {
		t.Error("restoring the content did not restore the key")
	}

	writeFile(t, filepath.Join(root, "outside.nix"), "{ b = 2; }\n")
	if treeKey(t, dir) == key {
		t.Error("an edit above the flake left the key alone, and ../ imports reach it")
	}
	writeFile(t, filepath.Join(root, "outside.nix"), "{ }\n")

	git("add", untracked)
	staged := treeKey(t, dir)
	if staged == key {
		t.Error("staging a new file left the key alone, and nix now sees it")
	}
	git("commit", "-q", "-m", "second")
	if treeKey(t, dir) == staged {
		t.Error("a commit left the key alone, and self.rev changed")
	}
}

func TestTreeKeyNoticesASameSizeEditRightAfterStaging(t *testing.T) {
	t.Parallel()
	requireGit(t)

	root, git := gitRepository(t)
	module := filepath.Join(root, "sub", "modules", "a.nix")
	writeFile(t, module, "{ x }\n")
	git("add", ".")
	key := treeKey(t, root)
	writeFile(t, module, "{ y }\n")
	if treeKey(t, root) == key {
		t.Error("a racy same-size edit left the key alone")
	}
}

func TestTreeKeySeparatesFlakesInOneRepository(t *testing.T) {
	t.Parallel()
	requireGit(t)

	root, _ := gitRepository(t)
	if treeKey(t, root) == treeKey(t, filepath.Join(root, "sub")) {
		t.Error("two flake directories of a repository share a key")
	}
}

func TestTreeKeyReadsEveryIndexForm(t *testing.T) {
	t.Parallel()
	requireGit(t)

	long := strings.Repeat("d", 100)
	deep := filepath.Join(long, long, long, long, long)
	forms := map[string][]string{
		"version 2":   {"update-index", "--index-version", "2"},
		"version 3":   {"update-index", "--index-version", "3"},
		"version 4":   {"update-index", "--index-version", "4"},
		"split index": {"update-index", "--split-index"},
		"intent":      {"add", "--intent-to-add", "intent.nix"},
	}
	for name, command := range forms {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			root, git := gitRepository(t)
			if err := os.MkdirAll(filepath.Join(root, deep), 0o700); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(root, deep, "long.nix"), "{ }\n")
			writeFile(t, filepath.Join(root, "intent.nix"), "{ }\n")
			git("add", deep)
			git(command...)
			key := treeKey(t, root)
			if treeKey(t, root) != key {
				t.Fatal("the key is not stable")
			}
			writeFile(t, filepath.Join(root, deep, "long.nix"), "{ a = 1; }\n")
			if treeKey(t, root) == key {
				t.Error("an edit of the file with a long path left the key alone")
			}
			writeFile(t, filepath.Join(root, "sub", "modules", "a.nix"), "{ b = 2; }\n")
			second := treeKey(t, root)
			git("commit", "-q", "--allow-empty", "-m", "next")
			if treeKey(t, root) == second {
				t.Error("a commit left the key alone")
			}
		})
	}
}

func TestTreeKeyMatchesAcrossIndexVersions(t *testing.T) {
	t.Parallel()
	requireGit(t)

	root, git := gitRepository(t)
	git("update-index", "--index-version", "2")
	second := treeKey(t, root)
	git("update-index", "--index-version", "4")
	if treeKey(t, root) != second {
		t.Error("the index version changed the key")
	}
}

func TestTreeKeyFindsALinkedWorktreeAndPackedReferences(t *testing.T) {
	t.Parallel()
	requireGit(t)

	root, git := gitRepository(t)
	linked := filepath.Join(t.TempDir(), "linked")
	git("worktree", "add", "-q", "-b", "other", linked)
	key := treeKey(t, linked)
	writeFile(t, filepath.Join(linked, "outside.nix"), "{ c = 3; }\n")
	if treeKey(t, linked) == key {
		t.Error("an edit in the linked worktree left its key alone")
	}
	mainKey := treeKey(t, root)
	git("pack-refs", "--all")
	if treeKey(t, root) != mainKey {
		t.Error("packing references changed the key")
	}
	git("commit", "-q", "--allow-empty", "-m", "next")
	git("pack-refs", "--all")
	if treeKey(t, root) == mainKey {
		t.Error("a packed commit left the key alone")
	}
}

func TestTreeKeyFollowsTrackedLinksAndExecutableBits(t *testing.T) {
	t.Parallel()
	requireGit(t)

	root, git := gitRepository(t)
	link := filepath.Join(root, "link.nix")
	if err := os.Symlink("outside.nix", link); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	key := treeKey(t, root)
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("sub/flake.nix", link); err != nil {
		t.Fatal(err)
	}
	if treeKey(t, root) == key {
		t.Error("retargeting a tracked link left the key alone")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if treeKey(t, root) == key {
		t.Error("deleting a tracked file left the key alone")
	}
}

func TestWalkedTreeKeyCoversEveryFileExceptGit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "flake.nix"), "{ }\n")
	key, err := walkTree(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	git := filepath.Join(dir, "vendored", gitDirName)
	if err := os.MkdirAll(git, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(git, "HEAD"), "x\n")
	if got, err := walkTree(t.Context(), dir); err != nil || got != key {
		t.Errorf("walkTree() = %x, %v, want the git directory left out", got, err)
	}
	writeFile(t, filepath.Join(dir, "data.json"), "{}\n")
	if got, err := walkTree(t.Context(), dir); err != nil || got == key {
		t.Errorf("walkTree() = %x, %v, want a data file to change the key", got, err)
	}
}

func TestFingerprintStopsWithItsContext(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "flake.nix"), "{ }\n")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := fingerprintTree(ctx, dir); !errors.Is(err, context.Canceled) {
		t.Errorf("fingerprintTree() error = %v, want cancelled", err)
	}
}

func TestEachIndexEntryRefusesDamage(t *testing.T) {
	t.Parallel()

	visit := func(indexEntry) error { return nil }
	for name, data := range map[string][]byte{
		"empty":          {},
		"not an index":   []byte("NOPE00000000"),
		"cut short":      []byte("DIRC\x00\x00\x00\x02\x00\x00\x00\x01"),
		"long extension": append([]byte("DIRC\x00\x00\x00\x02\x00\x00\x00\x00"), []byte("TREE\xFF\xFF\xFF\xFF"+strings.Repeat("x", 40))...),
	} {
		if err := eachIndexEntry(data, visit); err == nil {
			t.Errorf("%s: eachIndexEntry() = nil error", name)
		}
	}
	unknown := []byte("DIRC\x00\x00\x00\x09\x00\x00\x00\x00")
	if err := eachIndexEntry(unknown, visit); !errors.Is(err, errIndexUnsupported) {
		t.Errorf("unknown version: error = %v, want the walk to take over", err)
	}
}
