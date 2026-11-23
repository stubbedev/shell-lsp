package gitrepo

import (
	"os"
	"path/filepath"
	"testing"
)

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}
}

func TestDiscoverFollowsASymlinkedGitDirectory(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"store/git/HEAD":         "ref: refs/heads/main\n",
		"store/git/info/exclude": "excluded\n",
		"repo/sub/keep":          "",
	})
	symlink(t, f.path("store/git"), f.path("repo/.git"))
	repo, found, err := Discover(f.path("repo/sub"))
	if err != nil || !found || repo.Top != f.path("repo") || repo.linked {
		t.Fatalf("Discover = %+v, %v, %v; want the repository at %s read as a directory", repo, found, err, f.path("repo"))
	}
	assertIgnored(t, f.ignored(t, "repo/sub", "excluded", "kept"), "excluded")
}

func TestDiscoverPassesOverAMarkerThatIsNeitherADirectoryNorAFile(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{"repo/.git/HEAD": "", "repo/inner/keep": ""})
	symlink(t, f.path("missing"), f.path("repo/inner/.git"))
	repo, found, err := Discover(f.path("repo/inner"))
	if err != nil || !found || repo.Top != f.path("repo") {
		t.Errorf("Discover past a dangling .git link = %+v, %v, %v; want %s", repo, found, err, f.path("repo"))
	}
}

func locateIn(t *testing.T, f fixture, vars map[string]string, cwd, dir string) (Repository, Dirs, bool) {
	t.Helper()
	f.env.Git.Variables = vars
	f.env.Repositories = f.env.Git.Locate(f.path(cwd))
	ignores := f.ignores()
	repo, found, err := ignores.Repository(f.path(dir))
	if err != nil {
		t.Fatalf("Repository(%s) with %v from %s: %v", dir, vars, cwd, err)
	}
	if !found {
		return repo, Dirs{}, false
	}
	dirs, err := repo.Dirs(ignores.texts.Load)
	if err != nil {
		t.Fatal(err)
	}
	return repo, dirs, true
}

func TestLocateFollowsTheRepositoryVariablesOfGit(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"main/.git/HEAD":                   "ref: refs/heads/main\n",
		"main/.git/worktrees/wt/HEAD":      "ref: refs/heads/side\n",
		"main/.git/worktrees/wt/commondir": "../..\n",
		"wt/.git":                          "gitdir: ../main/.git/worktrees/wt\n",
		"wt/sub/keep":                      "",
		"other/.git/HEAD":                  "",
		"elsewhere/keep":                   "",
	})
	mainGit, wtGit := f.path("main/.git"), f.path("main/.git/worktrees/wt")
	for _, tt := range []struct {
		name     string
		vars     map[string]string
		cwd, dir string
		top      string
		dirs     Dirs
	}{
		{"no variables", nil, "elsewhere", "wt/sub", "wt", Dirs{Git: wtGit, Common: mainGit}},
		{"a git directory relative to the working directory", map[string]string{"GIT_DIR": filepath.Join("..", "..", "main", ".git", "worktrees", "wt")}, "wt/sub", "wt/sub", "wt/sub", Dirs{Git: wtGit, Common: mainGit}},
		{"a git link file", map[string]string{"GIT_DIR": filepath.Join("..", ".git")}, "wt/sub", "wt/sub", "wt/sub", Dirs{Git: wtGit, Common: mainGit}},
		{"a git directory and a work tree", map[string]string{"GIT_DIR": mainGit, "GIT_WORK_TREE": ".."}, "wt/sub", "wt", "wt", Dirs{Git: mainGit, Common: mainGit}},
		{"a work tree of the discovered repository", map[string]string{"GIT_WORK_TREE": f.path("elsewhere")}, "wt/sub", "elsewhere", "elsewhere", Dirs{Git: wtGit, Common: mainGit}},
		{"a common directory of the discovered repository", map[string]string{"GIT_COMMON_DIR": f.path("other/.git")}, "wt/sub", "wt/sub", "wt", Dirs{Git: wtGit, Common: f.path("other/.git")}},
	} {
		repo, dirs, found := locateIn(t, f, tt.vars, tt.cwd, tt.dir)
		if !found || repo.Top != f.path(tt.top) || dirs != tt.dirs {
			t.Errorf("%s: repository = %v, %+v, %v; want %s, %+v", tt.name, found, repo, dirs, f.path(tt.top), tt.dirs)
		}
	}
	if repo, _, found := locateIn(t, f, map[string]string{"GIT_DIR": mainGit}, "wt", "other"); found {
		t.Errorf("a directory outside the work tree GIT_DIR gives = %+v, want no repository", repo)
	}
}

func TestLocateRefusesRepositoryVariablesGitRefuses(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{"elsewhere/keep": "", "repo/.git/HEAD": ""})
	for name, vars := range map[string]map[string]string{
		"an empty git directory":           {"GIT_DIR": ""},
		"an empty work tree":               {"GIT_DIR": f.path("repo/.git"), "GIT_WORK_TREE": ""},
		"an empty common directory":        {"GIT_COMMON_DIR": ""},
		"a missing git directory":          {"GIT_DIR": f.path("missing")},
		"a work tree outside a repository": {"GIT_WORK_TREE": f.path("elsewhere")},
	} {
		f.env.Git.Variables = vars
		f.env.Repositories = f.env.Git.Locate(f.path("elsewhere"))
		if repo, _, err := f.ignores().Repository(f.path("elsewhere")); err == nil {
			t.Errorf("%s: repository = %+v, want an error", name, repo)
		}
	}
}

func TestBaseReadsTheExcludesOfTheCommonDirectoryGitIsGiven(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"repo/.git/info/exclude":  "own\n",
		"other/.git/info/exclude": "common\n",
	})
	f.env.Git.Variables = map[string]string{"GIT_COMMON_DIR": f.path("other/.git")}
	f.env.Repositories = f.env.Git.Locate(f.path("repo"))
	assertIgnored(t, f.ignored(t, "repo", "own", "common"), "common")
}

func TestRepositoryLookupsAreCachedUntilInvalidated(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{"repo/sub/keep": ""})
	ignores := f.ignores()
	if _, found, err := ignores.Repository(f.path("repo/sub")); found || err != nil {
		t.Fatalf("Repository before git init = %v, %v", found, err)
	}
	writeFiles(t, f.dir, map[string]string{"repo/.git/HEAD": ""})
	if _, found, _ := ignores.Repository(f.path("repo/sub")); found {
		t.Error("a repository lookup was repeated within the recheck interval")
	}
	ignores.Invalidate(f.path("repo/.git"))
	if repo, found, err := ignores.Repository(f.path("repo/sub")); !found || err != nil || repo.Top != f.path("repo") {
		t.Errorf("Repository after invalidating the new .git = %+v, %v, %v", repo, found, err)
	}
}
