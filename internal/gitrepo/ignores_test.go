package gitrepo

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/platform"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, text := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

type fixture struct {
	dir  string
	user platform.UserDirs
}

func newFixture(t *testing.T, files map[string]string) fixture {
	t.Helper()
	dir := t.TempDir()
	writeFiles(t, dir, files)
	return fixture{dir: dir, user: platform.UserDirs{Home: filepath.Join(dir, "home")}}
}

func (f fixture) ignored(t *testing.T, repo string, rels ...string) []string {
	t.Helper()
	ignores := NewIgnores(f.user, os.ReadFile, func() time.Time { return time.Time{} }, time.Hour)
	stack, err := ignores.Base(Repository{Top: filepath.Join(f.dir, filepath.FromSlash(repo))})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, rel := range rels {
		if stack.Ignored(rel, true) {
			out = append(out, rel)
		}
	}
	return out
}

func assertIgnored(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("ignored = %v, want %v", got, want)
	}
}

func TestBaseReadsTheDefaultExcludesFileWhenNoneIsSet(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"home/.config/git/ignore": "home-default\n",
		"xdg/git/ignore":          "xdg-default\n",
		"repo/.git/HEAD":          "ref: refs/heads/main\n",
	})
	assertIgnored(t, f.ignored(t, "repo", "home-default", "xdg-default"), "home-default")

	f.user.ConfigHome = filepath.Join(f.dir, "xdg")
	assertIgnored(t, f.ignored(t, "repo", "home-default", "xdg-default"), "xdg-default")
}

func TestBaseTakesTheLastExcludesFileSetAcrossConfigFiles(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"home/.config/git/config": "[core]\n\texcludesFile = ~/xdg-set\n",
		"home/xdg-set":            "from-xdg-config\n",
		"home/.gitconfig":         "[core]\n\texcludesFile = ~/home-set\n",
		"home/home-set":           "from-gitconfig\n",
		"home/.config/git/ignore": "from-default\n",
		"repo/.git/config":        "[core]\n\texcludesFile = repo-set\n",
		"repo/repo-set":           "from-repo\n",
		"other/.git/HEAD":         "",
	})
	all := []string{"from-xdg-config", "from-gitconfig", "from-default", "from-repo"}
	assertIgnored(t, f.ignored(t, "repo", all...), "from-repo")
	assertIgnored(t, f.ignored(t, "other", all...), "from-gitconfig")
}

func TestBaseFollowsIncludesAndOptionalPaths(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"home/.gitconfig":         "[include]\n\tpath = conf/extra\n\tpath = missing\n",
		"home/conf/extra":         "[core]\n\texcludesFile = :(optional)~/absent\n",
		"home/.config/git/ignore": "from-default\n",
		"repo/.git/HEAD":          "",
		"empty/.git/config":       "[core]\n\texcludesFile =\n",
	})
	assertIgnored(t, f.ignored(t, "repo", "from-default"), "from-default")
	assertIgnored(t, f.ignored(t, "empty", "from-default"))
}

func TestBaseRefusesAnExcludesFileItCannotPlace(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"home/.gitconfig":      "[includeIf \"gitdir:~/work/\"]\n\tpath = work\n",
		"home/work":            "[core]\n\texcludesFile = ~/work-ignore\n",
		"plain/.git/HEAD":      "",
		"override/.git/HEAD":   "",
		"override/.git/config": "[core]\n\texcludesFile = ~/repo-ignore\n",
		"home/repo-ignore":     "from-repo\n",
	})
	ignores := NewIgnores(f.user, os.ReadFile, func() time.Time { return time.Time{} }, time.Hour)
	if _, err := ignores.Base(Repository{Top: filepath.Join(f.dir, "plain")}); err == nil {
		t.Error("an excludesFile set only under includeIf was accepted")
	}
	assertIgnored(t, f.ignored(t, "override", "from-repo"), "from-repo")
}

func TestBaseLetsInfoExcludeOverrideTheExcludesFile(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"home/.config/git/ignore":          "build\nkept\n",
		"main/.git/info/exclude":           "!kept\n",
		"main/.git/worktrees/wt/commondir": "../..\n",
		"wt/.git":                          "gitdir: ../main/.git/worktrees/wt\n",
	})
	assertIgnored(t, f.ignored(t, "main", "build", "kept"), "build")
	ignores := NewIgnores(f.user, os.ReadFile, func() time.Time { return time.Time{} }, time.Hour)
	stack, err := ignores.Base(Repository{Top: filepath.Join(f.dir, "wt"), Linked: true})
	if err != nil {
		t.Fatal(err)
	}
	if !stack.Ignored("build", true) || stack.Ignored("kept", true) {
		t.Error("a linked worktree does not read the excludes of its common directory")
	}
}
