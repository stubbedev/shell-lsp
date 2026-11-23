package gitrepo

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/platform"
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
		"store/git/objects/":     "",
		"store/git/refs/":        "",
		"store/git/info/exclude": "excluded\n",
		"repo/sub/keep":          "",
	})
	symlink(t, f.path("store/git"), f.path("repo/.git"))
	repo, found, err := Discover(f.path("repo/sub"))
	if err != nil || !found || repo.Top != f.path("repo") || repo.Dirs.Git != f.path("repo/.git") {
		t.Fatalf("Discover = %+v, %v, %v; want the repository at %s read as a directory", repo, found, err, f.path("repo"))
	}
	assertIgnored(t, f.ignored(t, "repo/sub", "excluded", "kept"), "excluded")
}

func TestDiscoverPassesOverAMarkerThatIsNeitherADirectoryNorAFile(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{"repo/.git/HEAD": "ref: refs/heads/main\n", "repo/.git/objects/": "", "repo/.git/refs/": "", "repo/inner/keep": ""})
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
	return repo, repo.Dirs, true
}

func TestLocateFollowsTheRepositoryVariablesOfGit(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"main/.git/HEAD":                   "ref: refs/heads/main\n",
		"main/.git/objects/":               "",
		"main/.git/refs/":                  "",
		"main/.git/worktrees/wt/HEAD":      "ref: refs/heads/side\n",
		"main/.git/worktrees/wt/commondir": "../..\n",
		"wt/.git":                          "gitdir: ../main/.git/worktrees/wt\n",
		"wt/sub/keep":                      "",
		"other/.git/HEAD":                  "ref: refs/heads/main\n",
		"other/.git/objects/":              "",
		"other/.git/refs/":                 "",
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

	f := newFixture(t, map[string]string{"elsewhere/keep": "", "repo/.git/HEAD": "ref: refs/heads/main\n", "repo/.git/objects/": "", "repo/.git/refs/": ""})
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
		"other/.git/HEAD":         "ref: refs/heads/main\n",
		"other/.git/objects/":     "",
		"other/.git/refs/":        "",
		"repo/.git/HEAD":          "ref: refs/heads/main\n",
		"repo/.git/objects/":      "",
		"repo/.git/refs/":         "",
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
	writeFiles(t, f.dir, map[string]string{"repo/.git/HEAD": "ref: refs/heads/main\n", "repo/.git/objects/": "", "repo/.git/refs/": ""})
	if _, found, _ := ignores.Repository(f.path("repo/sub")); found {
		t.Error("a repository lookup was repeated within the recheck interval")
	}
	ignores.Invalidate(f.path("repo/.git"))
	if repo, found, err := ignores.Repository(f.path("repo/sub")); !found || err != nil || repo.Top != f.path("repo") {
		t.Errorf("Repository after invalidating the new .git = %+v, %v, %v", repo, found, err)
	}
}

func repoFiles(dirs ...string) map[string]string {
	files := map[string]string{}
	for _, dir := range dirs {
		files[dir+"/.git/HEAD"] = "ref: refs/heads/main\n"
		files[dir+"/.git/objects/"] = ""
		files[dir+"/.git/refs/"] = ""
	}
	return files
}

func discoverWith(t *testing.T, f fixture, vars map[string]string, dir string) (Repository, bool) {
	t.Helper()
	f.env.Git.Variables = vars
	f.env.Repositories = f.env.Git.Discovery()
	repo, found, err := f.ignores().Repository(f.path(dir))
	if err != nil {
		t.Fatalf("Repository(%s) with %v: %v", dir, vars, err)
	}
	return repo, found
}

func TestDiscoverPassesOverAGitDirectoryThatIsNotARepository(t *testing.T) {
	t.Parallel()

	for name, files := range map[string]map[string]string{
		"empty":                {"repo/sub/.git/": ""},
		"no HEAD":              {"repo/sub/.git/objects/": "", "repo/sub/.git/refs/": ""},
		"a HEAD naming no ref": {"repo/sub/.git/HEAD": "ref: heads/main\n", "repo/sub/.git/objects/": "", "repo/sub/.git/refs/": ""},
		"a short object id":    {"repo/sub/.git/HEAD": "012345678901234567890123456789012345678\n", "repo/sub/.git/objects/": "", "repo/sub/.git/refs/": ""},
		"no objects":           {"repo/sub/.git/HEAD": "ref: refs/heads/main\n", "repo/sub/.git/refs/": ""},
		"no refs":              {"repo/sub/.git/HEAD": "ref: refs/heads/main\n", "repo/sub/.git/objects/": ""},
	} {
		maps.Copy(files, repoFiles("repo"))
		f := newFixture(t, files)
		if repo, found := discoverWith(t, f, nil, "repo/sub"); !found || repo.Top != f.path("repo") {
			t.Errorf("%s: repository = %+v, %v; want the enclosing one at %s", name, repo, found, f.path("repo"))
		}
	}
	f := newFixture(t, map[string]string{
		"repo/.git/HEAD":     "0123456789012345678901234567890123456789 trailing\n",
		"repo/.git/objects/": "",
		"repo/.git/refs/":    "",
	})
	if repo, found := discoverWith(t, f, nil, "repo"); !found || repo.Top != f.path("repo") {
		t.Errorf("a detached HEAD: repository = %+v, %v", repo, found)
	}
}

func TestDiscoverRefusesAGitFileGitRefuses(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{"repo/.git": "gitdir: ../missing\n"})
	if repo, found, err := f.ignores().Repository(f.path("repo")); err == nil {
		t.Errorf("a .git file naming no repository = %+v, %v; want an error", repo, found)
	}
}

func TestDiscoverStopsBelowTheCeilingDirectories(t *testing.T) {
	t.Parallel()

	resolved := newFixture(t, repoFiles("repo"))
	writeFiles(t, resolved.dir, map[string]string{"repo/a/b/": ""})
	symlinked := filepath.Join(t.TempDir(), "link")
	symlink(t, resolved.path("repo"), symlinked)
	aliased := resolved
	aliased.dir = filepath.Join(t.TempDir(), "alias")
	symlink(t, resolved.dir, aliased.dir)
	sep := string(filepath.ListSeparator)
	for base, f := range map[string]fixture{"resolved": resolved, "behind a symbolic link": aliased} {
		for _, tt := range []struct {
			name    string
			ceiling string
			dir     string
			found   bool
		}{
			{"a ceiling below the repository", f.path("repo/a"), "repo/a/b", false},
			{"the repository as ceiling", f.path("repo"), "repo/a/b", false},
			{"a ceiling with a trailing separator", f.path("repo") + string(filepath.Separator), "repo/a/b", false},
			{"the start directory as ceiling", f.path("repo/a"), "repo/a", true},
			{"a ceiling below the start", f.path("repo/a/b/c"), "repo/a/b", true},
			{"a relative ceiling", "repo", "repo/a/b", true},
			{"a ceiling through a symbolic link", filepath.Join(symlinked, "a"), "repo/a/b", false},
			{"a ceiling after an empty entry is not resolved", sep + filepath.Join(symlinked, "a"), "repo/a/b", true},
			{"an empty list", "", "repo/a/b", true},
		} {
			if _, found := discoverWith(t, f, map[string]string{"GIT_CEILING_DIRECTORIES": tt.ceiling}, tt.dir); found != tt.found {
				t.Errorf("%s, %s: found = %v, want %v", base, tt.name, found, tt.found)
			}
		}
	}
}

func TestDiscoverStopsAtAFilesystemBoundary(t *testing.T) {
	t.Parallel()

	f := newFixture(t, repoFiles("repo"))
	writeFiles(t, f.dir, map[string]string{"repo/mount/sub/": ""})
	mount := f.path("repo/mount")
	device := func(path string) (uint64, error) {
		if platform.Native.Within(path, mount) {
			return 2, nil
		}
		return 1, nil
	}
	p := probe{track: func(string) {}, device: device}
	for _, tt := range []struct {
		across string
		found  bool
	}{{"", false}, {"false", false}, {"true", true}, {"1", true}} {
		f.env.Git.Variables = map[string]string{"GIT_DISCOVERY_ACROSS_FILESYSTEM": tt.across}
		_, found, err := f.env.Git.Discovery().find(f.path("repo/mount/sub"), p)
		if err != nil || found != tt.found {
			t.Errorf("GIT_DISCOVERY_ACROSS_FILESYSTEM=%q: found = %v, %v; want %v", tt.across, found, err, tt.found)
		}
	}
	if _, found, err := (Locator{}).find(f.path("repo/mount/sub"), p); err != nil || found {
		t.Errorf("unset: found = %v, %v; want the search to stop at the boundary", found, err)
	}
	f.env.Git.Variables = map[string]string{"GIT_DISCOVERY_ACROSS_FILESYSTEM": "maybe"}
	if _, _, err := f.env.Git.Discovery().find(f.path("repo"), p); !errors.Is(err, errBadBoolean) {
		t.Errorf("a bad boolean: error = %v, want %v", err, errBadBoolean)
	}
}

func TestDiscoverHonoursCoreWorktree(t *testing.T) {
	t.Parallel()

	const versioned = "[core]\n\trepositoryformatversion = 0\n"
	files := repoFiles("repo", "unversioned", "bare", "linked", "missing", "empty")
	maps.Copy(files, map[string]string{
		"repo/.git/config":                   versioned + "\tworktree = ../sub\n",
		"repo/sub/deep/":                     "",
		"elsewhere/":                         "",
		"unversioned/.git/config":            "[core]\n\tworktree = ../sub\n",
		"unversioned/sub/":                   "",
		"bare/.git/config":                   versioned + "\tbare\n",
		"bare/sub/":                          "",
		"missing/.git/config":                versioned + "\tworktree = ../gone\n",
		"empty/.git/config":                  versioned + "\tworktree\n",
		"linked/.git/config":                 versioned + "\tworktree = ../../elsewhere\n",
		"linked/.git/worktrees/wt/HEAD":      "ref: refs/heads/wt\n",
		"linked/.git/worktrees/wt/commondir": "../..\n",
		"wt/.git":                            "gitdir: ../linked/.git/worktrees/wt\n",
		"module/.git":                        "gitdir: ../store/modules/m\n",
		"store/modules/m/HEAD":               "ref: refs/heads/main\n",
		"store/modules/m/objects/":           "",
		"store/modules/m/refs/":              "",
		"store/modules/m/config":             versioned + "\tworktree = ../../../module\n",
	})
	f := newFixture(t, files)
	f.env.Git.Variables = map[string]string{"GIT_DIR": f.path("repo/.git")}
	cwdLocator := f.env.Git.Locate(f.path("elsewhere"))
	for _, tt := range []struct {
		name, dir, top string
	}{
		{"a work tree inside the directory holding .git", "repo/sub/deep", "repo/sub"},
		{"a repository without a format version", "unversioned/sub", "unversioned"},
		{"a linked worktree", "wt", "wt"},
		{"a submodule", "module", "module"},
	} {
		repo, found := discoverWith(t, f, nil, tt.dir)
		if !found || repo.Top != f.path(tt.top) {
			t.Errorf("%s: repository = %+v, %v; want the top %s", tt.name, repo, found, f.path(tt.top))
		}
	}
	for name, dir := range map[string]string{"outside the work tree": "repo", "a bare repository": "bare/sub"} {
		if repo, found := discoverWith(t, f, nil, dir); found {
			t.Errorf("%s: repository = %+v, want none", name, repo)
		}
	}
	for _, dir := range []string{"missing", "empty"} {
		if repo, _, err := f.ignores().Repository(f.path(dir)); err == nil {
			t.Errorf("%s: repository = %+v, want an error as git gives", dir, repo)
		}
	}
	repo, found, err := cwdLocator.find(f.path("repo/sub/deep"), probe{track: func(string) {}, device: platform.DeviceOf})
	if err != nil || !found || repo.Top != f.path("repo/sub") {
		t.Errorf("GIT_DIR with core.worktree = %+v, %v, %v; want the top %s", repo, found, err, f.path("repo/sub"))
	}
}

func TestInvalidatingAFileInAGitDirectoryDropsTheLookup(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{"repo/.git/objects/": "", "repo/.git/refs/": "", "repo/sub/": ""})
	ignores := f.ignores()
	if _, found, err := ignores.Repository(f.path("repo/sub")); found || err != nil {
		t.Fatalf("Repository without HEAD = %v, %v", found, err)
	}
	writeFiles(t, f.dir, map[string]string{"repo/.git/HEAD": "ref: refs/heads/main\n"})
	ignores.Invalidate(f.path("repo/.git/HEAD"))
	if repo, found, err := ignores.Repository(f.path("repo/sub")); !found || err != nil || repo.Top != f.path("repo") {
		t.Errorf("Repository after HEAD appears = %+v, %v, %v", repo, found, err)
	}
}

func TestDiscoverWalksTheResolvedParentsOfTheDirectory(t *testing.T) {
	t.Parallel()

	files := repoFiles("a", "b")
	files["b/x/deep/"] = ""
	files["links/"] = ""
	f := newFixture(t, files)
	symlink(t, f.path("b/x"), f.path("a/sub"))
	symlink(t, f.path("b/x"), f.path("links/x"))
	symlink(t, f.path("b"), f.path("via"))
	for _, tt := range []struct {
		name, dir, top, prefix, git string
	}{
		{"a symlink inside another work tree", "a/sub", "b", "x", "b/.git"},
		{"a symlink outside any work tree", "links/x/deep", "b", filepath.Join("x", "deep"), "b/.git"},
		{"a symlinked parent of the work tree", "via/x/deep", "via", filepath.Join("x", "deep"), "b/.git"},
		{"the top through a symlinked parent", "via", "via", ".", "via/.git"},
	} {
		repo, found, err := Discover(f.path(tt.dir))
		if err != nil || !found || repo.Top != f.path(tt.top) || repo.Prefix != tt.prefix || repo.Dirs.Git != f.path(tt.git) {
			t.Errorf("%s: Discover = %+v, %v, %v; want the top %s, prefix %s and git directory %s", tt.name, repo, found, err, f.path(tt.top), tt.prefix, f.path(tt.git))
		}
	}
}

func TestDiscoverStopsAtABareRepository(t *testing.T) {
	t.Parallel()

	const versioned = "[core]\n\trepositoryformatversion = 0\n"
	files := repoFiles("outer", "both")
	maps.Copy(files, map[string]string{
		"outer/bare/HEAD":        "ref: refs/heads/main\n",
		"outer/bare/objects/":    "",
		"outer/bare/refs/":       "",
		"outer/bare/sub/":        "",
		"outer/nonbare/HEAD":     "ref: refs/heads/main\n",
		"outer/nonbare/objects/": "",
		"outer/nonbare/refs/":    "",
		"outer/nonbare/config":   versioned + "\tbare = false\n",
		"outer/moved/HEAD":       "ref: refs/heads/main\n",
		"outer/moved/objects/":   "",
		"outer/moved/refs/":      "",
		"outer/moved/config":     versioned + "\tbare = false\n\tworktree = ../wt\n",
		"outer/wt/deep/":         "",
		"both/HEAD":              "ref: refs/heads/main\n",
		"both/objects/":          "",
		"both/refs/":             "",
	})
	f := newFixture(t, files)
	for _, dir := range []string{"outer/bare", "outer/bare/sub", "outer/bare/objects", "outer/.git/refs", "outer/nonbare", "outer/moved"} {
		if repo, found := discoverWith(t, f, nil, dir); found {
			t.Errorf("%s: repository = %+v, want none, as git finds a repository without a work tree", dir, repo)
		}
	}
	if repo, found := discoverWith(t, f, nil, "outer/wt/deep"); !found || repo.Top != f.path("outer") {
		t.Errorf("outer/wt/deep: repository = %+v, %v; want the enclosing one, as git does not look for bare repositories below", repo, found)
	}
	if repo, found := discoverWith(t, f, nil, "both"); !found || repo.Dirs.Git != f.path("both/.git") {
		t.Errorf("both: repository = %+v, %v; want the .git directory before the directory itself", repo, found)
	}
	repo, dirs, found := locateIn(t, f, map[string]string{"GIT_WORK_TREE": f.path("outer/wt")}, "outer/bare", "outer/wt/deep")
	if !found || repo.Top != f.path("outer/wt") || dirs.Git != f.path("outer/bare") {
		t.Errorf("GIT_WORK_TREE in a bare repository: repository = %+v, %v; want the work tree %s", repo, found, f.path("outer/wt"))
	}
}

func TestDiscoverChangesIntoCoreWorktreeFromTheResolvedGitDirectory(t *testing.T) {
	t.Parallel()

	const versioned = "[core]\n\trepositoryformatversion = 0\n"
	f := newFixture(t, map[string]string{
		"store/git/HEAD":     "ref: refs/heads/main\n",
		"store/git/objects/": "",
		"store/git/refs/":    "",
		"store/git/config":   versioned + "\tworktree = ../sub\n",
		"store/sub/deep/":    "",
		"repo/sub/":          "",
		"abs/.git/HEAD":      "ref: refs/heads/main\n",
		"abs/.git/objects/":  "",
		"abs/.git/refs/":     "",
		"real/wt/deep/":      "",
	})
	symlink(t, f.path("store/git"), f.path("repo/.git"))
	symlink(t, f.path("real"), f.path("link"))
	f.env.Git.Variables = map[string]string{"GIT_DIR": f.path("repo/.git")}
	locator := f.env.Git.Locate(f.path("store/sub/deep"))
	p := probe{track: func(string) {}, device: platform.DeviceOf}
	repo, found, err := locator.find(f.path("store/sub/deep"), p)
	if err != nil || !found || repo.Top != f.path("store/sub") || repo.Prefix != "deep" {
		t.Errorf("a relative core.worktree through a symlinked git directory = %+v, %v, %v; want the top %s", repo, found, err, f.path("store/sub"))
	}
	if repo, found, err := locator.find(f.path("repo/sub"), p); err != nil || found {
		t.Errorf("the work tree as written = %+v, %v, %v; want none", repo, found, err)
	}

	writeFiles(t, f.dir, map[string]string{"abs/.git/config": versioned + "\tworktree = " + filepath.ToSlash(f.path("link/wt")) + "\n"})
	f.env.Git.Variables = map[string]string{"GIT_DIR": f.path("abs/.git")}
	locator = f.env.Git.Locate(f.path("real/wt"))
	for dir, top := range map[string]string{"real/wt/deep": "real/wt", "link/wt/deep": "link/wt"} {
		if repo, found, err := locator.find(f.path(dir), p); err != nil || !found || repo.Top != f.path(top) || repo.Prefix != "deep" {
			t.Errorf("%s with an absolute core.worktree through a symlink = %+v, %v, %v; want the top %s", dir, repo, found, err, f.path(top))
		}
	}

	writeFiles(t, f.dir, map[string]string{"abs/.git/config": versioned + "\tworktree = " + filepath.ToSlash(f.path("gone")) + "\n"})
	if repo, found, err := locator.find(f.path("real/wt"), p); err != nil || found {
		t.Errorf("a missing absolute core.worktree = %+v, %v, %v; want no work tree, as git finds none", repo, found, err)
	}
}
