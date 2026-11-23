package gitlink

import (
	"path/filepath"
	"testing"
)

func TestTargetResolvesTheGitDirectoryOfALink(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "worktree")
	absolute := filepath.Join(t.TempDir(), "repo", ".git", "worktrees", "w")
	for _, tt := range []struct{ text, want string }{
		{"gitdir: ../repo/.git/worktrees/w\n", filepath.Join(filepath.Dir(root), "repo", ".git", "worktrees", "w")},
		{"gitdir:" + absolute, absolute},
		{"  gitdir:   sub/../mod  \n", filepath.Join(root, "mod")},
	} {
		got, err := Target(filepath.Join(root, ".git"), tt.text)
		if err != nil || got != tt.want {
			t.Errorf("Target(%q) = %q, %v; want %q", tt.text, got, err, tt.want)
		}
	}
	if _, err := Target(filepath.Join(root, ".git"), "ref: refs/heads/main\n"); err == nil {
		t.Error("a .git file without a gitdir line was accepted")
	}
}

func TestResolveKeepsAbsolutePathsAndJoinsRelativeOnes(t *testing.T) {
	t.Parallel()

	base := filepath.Join(t.TempDir(), "repo", ".git", "worktrees", "w")
	if got, want := Resolve(base, "../..\n"), filepath.Join(base, "..", ".."); got != want {
		t.Errorf("Resolve(../..) = %q, want %q", got, want)
	}
	if got := Resolve(base, base); got != base {
		t.Errorf("Resolve of an absolute path = %q, want %q", got, base)
	}
}
