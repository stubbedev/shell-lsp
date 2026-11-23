package gitrepo

import (
	"path/filepath"
	"testing"
)

func TestReadGitFileReadsTheGitDirectoryAsGitDoes(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"repo/.git/HEAD":                  "ref: refs/heads/main\n",
		"repo/.git/objects/":              "",
		"repo/.git/refs/":                 "",
		"repo/.git/worktrees/w/HEAD":      "ref: refs/heads/w\n",
		"repo/.git/worktrees/w/commondir": "../..\r\n",
		"empty/.git/":                     "",
	})
	link := f.path("w/.git")
	for _, tt := range []struct{ text, want string }{
		{"gitdir: ../repo/.git/worktrees/w\n", f.path("repo/.git/worktrees/w")},
		{"gitdir: " + f.path("repo/.git/worktrees/w") + "\r\n", f.path("repo/.git/worktrees/w")},
		{"gitdir: sub/../../repo/.git", f.path("repo/.git")},
	} {
		writeFiles(t, f.dir, map[string]string{"w/.git": tt.text})
		got, err := readGitFile(link, "", probe{track: func(string) {}})
		if err != nil || got.Git != tt.want || got.Common != f.path("repo/.git") {
			t.Errorf("readGitFile(%q) = %+v, %v; want %s in %s", tt.text, got, err, tt.want, f.path("repo/.git"))
		}
	}
	for _, text := range []string{"ref: refs/heads/main\n", "gitdir:../repo/.git\n", "  gitdir: ../repo/.git\n", "gitdir: \n", "gitdir: ../empty/.git\n", "gitdir: ../missing\n"} {
		writeFiles(t, f.dir, map[string]string{"w/.git": text})
		if got, err := readGitFile(link, "", probe{track: func(string) {}}); err == nil {
			t.Errorf("readGitFile(%q) = %+v, want an error as git gives", text, got)
		}
	}
}

func TestResolveLinkKeepsAbsolutePathsAndJoinsRelativeOnes(t *testing.T) {
	t.Parallel()

	base := filepath.Join(t.TempDir(), "repo", ".git", "worktrees", "w")
	if got, want := resolveLink(base, "../.."), filepath.Join(base, "..", ".."); got != want {
		t.Errorf("resolveLink(../..) = %q, want %q", got, want)
	}
	if got := resolveLink(base, base); got != base {
		t.Errorf("Resolve of an absolute path = %q, want %q", got, base)
	}
}
