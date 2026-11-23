package gitrepo

import (
	"errors"
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
	dir string
	env Environment
}

func newFixture(t *testing.T, files map[string]string) fixture {
	t.Helper()
	dir := t.TempDir()
	writeFiles(t, dir, files)
	return fixture{dir: dir, env: Environment{User: platform.UserDirs{Home: filepath.Join(dir, "home")}}}
}

func (f fixture) path(rel string) string { return filepath.Join(f.dir, filepath.FromSlash(rel)) }

func (f fixture) base(repo string, linked bool) (*Ignores, error) {
	ignores := NewIgnores(f.env, os.ReadFile, func() time.Time { return time.Time{} }, time.Hour)
	_, err := ignores.Base(Repository{Top: f.path(repo), Linked: linked})
	return ignores, err
}

func (f fixture) ignoredIn(t *testing.T, repo string, linked bool, rels ...string) []string {
	t.Helper()
	ignores := NewIgnores(f.env, os.ReadFile, func() time.Time { return time.Time{} }, time.Hour)
	return ignoredBy(t, ignores, Repository{Top: f.path(repo), Linked: linked}, rels...)
}

func ignoredBy(t *testing.T, ignores *Ignores, repo Repository, rels ...string) []string {
	t.Helper()
	stack, err := ignores.Base(repo)
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

func (f fixture) ignored(t *testing.T, repo string, rels ...string) []string {
	t.Helper()
	return f.ignoredIn(t, repo, false, rels...)
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

	f.env.User.ConfigHome = f.path("xdg")
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
		"optional/.git/config":    "[core]\n\texcludesFile = :(optional)\n",
	})
	assertIgnored(t, f.ignored(t, "repo", "from-default"), "from-default")
	assertIgnored(t, f.ignored(t, "empty", "from-default"))
	assertIgnored(t, f.ignored(t, "optional", "from-default"), "from-default")
}

func TestBaseExpandsEveryExcludesFileItReads(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"home/.gitconfig":      "[core]\n\texcludesFile = ~nobody/ignore\n",
		"repo/.git/config":     "[core]\n\texcludesFile = ~/repo-ignore\n",
		"home/repo-ignore":     "from-repo\n",
		"prefixed/.git/config": "[core]\n\texcludesFile = %(prefix)/etc/ignore\n",
		"prefix/etc/ignore":    "from-prefix\n",
	})
	f.env.Git.UserHome = func(string) (string, error) { return "", errNotExpanded }
	if _, err := f.base("repo", false); !errors.Is(err, errNotExpanded) {
		t.Errorf("an unknown user overridden by a later excludesFile = %v, want %v as git reports it", err, errNotExpanded)
	}

	f.env.Git.UserHome = func(string) (string, error) { return f.path("home"), nil }
	f.env.Git.Prefix = func() (string, error) { return f.path("prefix"), nil }
	assertIgnored(t, f.ignored(t, "prefixed", "from-prefix", "from-repo"), "from-prefix")
}

func TestBaseEvaluatesGitdirConditions(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"home/.gitconfig": "[includeIf \"gitdir:~/work/\"]\n\tpath = work.inc\n" +
			"[includeIf \"gitdir/i:~/WORK/upper/\"]\n\tpath = upper.inc\n" +
			"[includeIf \"gitdir:WORK/exact/\"]\n\tpath = never.inc\n" +
			"[includeIf \"gitdir:named/.git\"]\n\tpath = named.inc\n" +
			"[includeIf \"gitdir:./work/rel/\"]\n\tpath = relative.inc\n" +
			"[includeIf \"gitdir:~/work/glob*/\"]\n\tpath = glob.inc\n",
		"home/work.inc":               "[core]\n\texcludesFile = ~/work-ignore\n",
		"home/upper.inc":              "[core]\n\texcludesFile = ~/upper-ignore\n",
		"home/never.inc":              "[core]\n\texcludesFile = ~/never-ignore\n",
		"home/named.inc":              "[core]\n\texcludesFile = ~/named-ignore\n",
		"home/relative.inc":           "[core]\n\texcludesFile = ~/relative-ignore\n",
		"home/glob.inc":               "[core]\n\texcludesFile = ~/glob-ignore\n",
		"home/work-ignore":            "work\n",
		"home/upper-ignore":           "upper\n",
		"home/never-ignore":           "never\n",
		"home/named-ignore":           "named\n",
		"home/relative-ignore":        "relative\n",
		"home/glob-ignore":            "glob\n",
		"home/work/a/.git/HEAD":       "",
		"home/work/upper/.git/HEAD":   "",
		"home/work/exact/.git/HEAD":   "",
		"deep/x/named/.git/HEAD":      "",
		"home/work/rel/.git/HEAD":     "",
		"home/work/globbed/.git/HEAD": "",
		"elsewhere/.git/HEAD":         "",
	})
	all := []string{"work", "upper", "never", "named", "relative", "glob"}
	assertIgnored(t, f.ignored(t, "home/work/a", all...), "work")
	assertIgnored(t, f.ignored(t, "home/work/upper", all...), "upper")
	assertIgnored(t, f.ignored(t, "home/work/exact", all...), "work")
	assertIgnored(t, f.ignored(t, "deep/x/named", all...), "named")
	assertIgnored(t, f.ignored(t, "home/work/rel", all...), "relative")
	assertIgnored(t, f.ignored(t, "home/work/globbed", all...), "glob")
	assertIgnored(t, f.ignored(t, "elsewhere", all...))
}

func TestBaseMatchesGitdirAgainstTheResolvedAndTheGivenPath(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"real/repo/.git/HEAD": "",
		"home/ignore":         "linked\n",
	})
	if err := os.Symlink(f.path("real"), f.path("link")); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}
	writeFiles(t, f.dir, map[string]string{
		"home/.gitconfig": "[includeIf \"gitdir:" + filepath.ToSlash(f.path("link")) + "/\"]\n\tpath = linked\n",
		"home/linked":     "[core]\n\texcludesFile = ~/ignore\n",
	})
	assertIgnored(t, f.ignored(t, "link/repo", "linked"), "linked")
	assertIgnored(t, f.ignored(t, "real/repo", "linked"))
}

func TestBaseRefusesARelativeGitdirConditionOutsideAFile(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{"repo/.git/HEAD": ""})
	f.env.Git.Variables = map[string]string{
		"GIT_CONFIG_COUNT":   "1",
		"GIT_CONFIG_KEY_0":   "includeIf.gitdir:./x/.path",
		"GIT_CONFIG_VALUE_0": "/nowhere",
	}
	if _, err := f.base("repo", false); !errors.Is(err, errRelativeCondition) {
		t.Errorf("Base = %v, want %v", err, errRelativeCondition)
	}
}

func TestBaseEvaluatesOnbranchConditions(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"home/.gitconfig": "[includeIf \"onbranch:main\"]\n\tpath = main\n" +
			"[includeIf \"onbranch:feature/\"]\n\tpath = feature\n" +
			"[includeIf \"onbranch:*\"]\n\tpath = any\n",
		"home/main":           "[core]\n\texcludesFile = ~/main-ignore\n",
		"home/feature":        "[core]\n\texcludesFile = ~/feature-ignore\n",
		"home/any":            "[core]\n\texcludesFile = ~/any-ignore\n",
		"home/main-ignore":    "main\n",
		"home/feature-ignore": "feature\n",
		"home/any-ignore":     "any\n",
		"main/.git/HEAD":      "ref: refs/heads/main\n",
		"feature/.git/HEAD":   "ref:refs/heads/feature/deep/x\n",
		"other/.git/HEAD":     "ref: refs/heads/other\n",
		"detached/.git/HEAD":  "0123456789012345678901234567890123456789\n",
		"tag/.git/HEAD":       "ref: refs/tags/main\n",
		"reftable/.git/HEAD":  "ref: refs/heads/.invalid\n",
		"reftable/.git/config": "[core]\n\trepositoryformatversion = 1\n" +
			"[extensions]\n\trefStorage = reftable\n",
	})
	all := []string{"main", "feature", "any"}
	assertIgnored(t, f.ignored(t, "main", all...), "any")
	assertIgnored(t, f.ignored(t, "feature", all...), "feature")
	assertIgnored(t, f.ignored(t, "other", all...), "any")
	assertIgnored(t, f.ignored(t, "detached", all...))
	assertIgnored(t, f.ignored(t, "tag", all...))
	if _, err := f.base("reftable", false); !errors.Is(err, errReftable) {
		t.Errorf("onbranch in a reftable repository = %v, want %v", err, errReftable)
	}
}

func TestBaseEvaluatesRemoteURLConditionsOverEveryConfigFile(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"home/.gitconfig":     "[includeIf \"hasconfig:remote.*.url:https://example.com/**\"]\n\tpath = team\n",
		"home/team":           "[core]\n\texcludesFile = ~/team-ignore\n",
		"home/team-ignore":    "team\n",
		"team/.git/config":    "[remote \"origin\"]\n\turl = https://example.com/team/repo.git\n",
		"other/.git/config":   "[remote \"origin\"]\n\turl = https://example.org/repo.git\n",
		"shallow/.git/config": "[remote \"origin\"]\n\turl = https://example.com/repo.git\n",
	})
	assertIgnored(t, f.ignored(t, "team", "team"), "team")
	assertIgnored(t, f.ignored(t, "other", "team"))
	assertIgnored(t, f.ignored(t, "shallow", "team"), "team")

	f.env.Git.Variables = map[string]string{"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "remote.x.url", "GIT_CONFIG_VALUE_0": "https://example.com/x"}
	assertIgnored(t, f.ignored(t, "other", "team"), "team")
}

func TestBaseRefusesRemoteURLsUnderAnIncludeIfWhileHasconfigIsUsed(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"home/.gitconfig": "[includeIf \"gitdir:**\"]\n\tpath = any\n" +
			"[includeIf \"hasconfig:remote.*.url:nothing\"]\n\tpath = none\n",
		"home/any":       "[include]\n\tpath = nested\n",
		"home/nested":    "[remote \"x\"]\n\turl = https://example.com/x\n",
		"home/none":      "",
		"repo/.git/HEAD": "",
	})
	if _, err := f.base("repo", false); !errors.Is(err, errURLUnderCondition) {
		t.Errorf("Base = %v, want %v", err, errURLUnderCondition)
	}

	writeFiles(t, f.dir, map[string]string{"home/.gitconfig": "[includeIf \"gitdir:**\"]\n\tpath = any\n"})
	if _, err := f.base("repo", false); err != nil {
		t.Errorf("a remote URL under includeIf without a hasconfig condition was refused: %v", err)
	}
}

func TestBaseReadsWorktreeConfigOnlyWithTheExtension(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"home/.gitconfig":                        "[extensions]\n\tworktreeConfig = true\n",
		"off/.git/config":                        "[core]\n\texcludesFile = repo-ignore\n",
		"off/.git/config.worktree":               "[core]\n\texcludesFile = worktree-ignore\n",
		"off/repo-ignore":                        "repo\n",
		"off/worktree-ignore":                    "worktree\n",
		"main/.git/config":                       "[core]\n\texcludesFile = repo-ignore\n[extensions]\n\tworktreeConfig\n",
		"main/.git/config.worktree":              "[core]\n\texcludesFile = worktree-ignore\n",
		"main/repo-ignore":                       "repo\n",
		"main/worktree-ignore":                   "worktree\n",
		"main/.git/worktrees/wt/commondir":       "../..\n",
		"main/.git/worktrees/wt/config.worktree": "[core]\n\texcludesFile = linked-ignore\n",
		"wt/.git":                                "gitdir: ../main/.git/worktrees/wt\n",
		"wt/linked-ignore":                       "linked\n",
		"bad/.git/config":                        "[extensions]\n\tworktreeConfig = maybe\n",
	})
	all := []string{"repo", "worktree", "linked"}
	assertIgnored(t, f.ignored(t, "off", all...), "repo")
	assertIgnored(t, f.ignored(t, "main", all...), "worktree")
	assertIgnored(t, f.ignoredIn(t, "wt", true, all...), "linked")
	if _, err := f.base("bad", false); !errors.Is(err, errBadBoolean) {
		t.Errorf("Base = %v, want %v", err, errBadBoolean)
	}
}

func TestBaseMatchesConditionsOfALinkedWorktreeAgainstItsOwnGitDirectory(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"home/.gitconfig": "[includeIf \"gitdir:**/.git/worktrees/wt\"]\n\tpath = linked\n" +
			"[includeIf \"onbranch:topic\"]\n\tpath = topic\n",
		"home/linked":                      "[core]\n\texcludesFile = ~/linked-ignore\n",
		"home/linked-ignore":               "linked\n",
		"home/topic":                       "[include]\n\tpath = topic-extra\n",
		"home/topic-extra":                 "[core]\n\texcludesFile = ~/topic-ignore\n",
		"home/topic-ignore":                "topic\n",
		"main/.git/HEAD":                   "ref: refs/heads/topic\n",
		"main/.git/worktrees/wt/HEAD":      "ref: refs/heads/side\n",
		"main/.git/worktrees/wt/commondir": "../..\n",
		"wt/.git":                          "gitdir: ../main/.git/worktrees/wt\n",
	})
	assertIgnored(t, f.ignored(t, "main", "linked", "topic"), "topic")
	assertIgnored(t, f.ignoredIn(t, "wt", true, "linked", "topic"), "linked")
}

func TestBaseReadsTheSystemConfigAndTheConfigVariables(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"etc/gitconfig":   "[core]\n\texcludesFile = ~/system\n",
		"other/system":    "[core]\n\texcludesFile = ~/other-system\n",
		"home/.gitconfig": "[core]\n\texcludesFile = ~/home\n",
		"env/global":      "[core]\n\texcludesFile = ~/env-global\n",
		"repo/.git/HEAD":  "",
	})
	f.env.Git.SystemConfig = f.path("etc/gitconfig")
	last := func(vars map[string]string) (string, error) {
		t.Helper()
		f.env.Git.Variables = vars
		ignores := NewIgnores(f.env, os.ReadFile, func() time.Time { return time.Time{} }, time.Hour)
		if ignores.scopesErr != nil {
			return "", ignores.scopesErr
		}
		dirs := Dirs{Git: f.path("repo/.git"), Common: f.path("repo/.git")}
		w := walker{g: ignores, top: f.path("repo"), dirs: dirs, sources: ignores.sources(f.path("repo"), dirs, repositoryFormat{})}
		if err := w.run(); err != nil || !w.last.set {
			return "", err
		}
		return w.last.named.path, nil
	}
	home := func(name string) string { return f.path("home/" + name) }
	for _, tt := range []struct {
		name string
		vars map[string]string
		want string
	}{
		{"home wins over system", nil, home("home")},
		{"no system", map[string]string{"GIT_CONFIG_NOSYSTEM": "yes", "GIT_CONFIG_GLOBAL": ""}, ""},
		{"system without home", map[string]string{"GIT_CONFIG_NOSYSTEM": "0", "GIT_CONFIG_GLOBAL": ""}, home("system")},
		{"another system file", map[string]string{"GIT_CONFIG_SYSTEM": f.path("other/system"), "GIT_CONFIG_GLOBAL": ""}, home("other-system")},
		{"no system file", map[string]string{"GIT_CONFIG_SYSTEM": "", "GIT_CONFIG_GLOBAL": ""}, ""},
		{"another global file", map[string]string{"GIT_CONFIG_GLOBAL": f.path("env/global")}, home("env-global")},
		{"relative global file", map[string]string{"GIT_CONFIG_GLOBAL": filepath.Join("..", "env", "global")}, home("env-global")},
		{"command line wins", map[string]string{
			"GIT_CONFIG_COUNT":   "2",
			"GIT_CONFIG_KEY_0":   "core.excludesFile",
			"GIT_CONFIG_VALUE_0": "~/first",
			"GIT_CONFIG_KEY_1":   "Core.ExcludesFile",
			"GIT_CONFIG_VALUE_1": "~/second",
		}, home("second")},
		{"parameters come after the count", map[string]string{
			"GIT_CONFIG_PARAMETERS": "'user.name'='x' 'core.excludesFile'='~/it'\\''s' 'user.flag'= 'old.style=1'",
			"GIT_CONFIG_COUNT":      "1",
			"GIT_CONFIG_KEY_0":      "core.excludesFile",
			"GIT_CONFIG_VALUE_0":    "~/counted",
		}, home("it's")},
		{"an empty count", map[string]string{"GIT_CONFIG_COUNT": "", "GIT_CONFIG_KEY_0": "core.excludesFile", "GIT_CONFIG_VALUE_0": "~/x"}, home("home")},
	} {
		got, err := last(tt.vars)
		if err != nil || got != tt.want {
			t.Errorf("%s: excludesFile = %q, %v; want %q", tt.name, got, err, tt.want)
		}
	}
	for name, vars := range map[string]map[string]string{
		"bad nosystem":           {"GIT_CONFIG_NOSYSTEM": "maybe"},
		"bad count":              {"GIT_CONFIG_COUNT": "x"},
		"negative count":         {"GIT_CONFIG_COUNT": "-1"},
		"missing key":            {"GIT_CONFIG_COUNT": "1"},
		"missing value":          {"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "core.excludesFile"},
		"bad key":                {"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "nodot", "GIT_CONFIG_VALUE_0": "x"},
		"relative include":       {"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "include.path", "GIT_CONFIG_VALUE_0": "x"},
		"bad parameters":         {"GIT_CONFIG_PARAMETERS": "core.excludesfile=x"},
		"valueless parameter":    {"GIT_CONFIG_PARAMETERS": "'core.excludesfile'="},
		"unterminated parameter": {"GIT_CONFIG_PARAMETERS": "'core.excludesfile"},
		"junk after a parameter": {"GIT_CONFIG_PARAMETERS": "'core.excludesfile'='x'y"},
	} {
		if got, err := last(vars); err == nil {
			t.Errorf("%s: excludesFile = %q, want an error", name, got)
		}
	}
}

func TestBaseReadsTheDefaultExcludesFileWhicheverGlobalConfigIsRead(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"home/.config/git/ignore": "default\n",
		"repo/.git/HEAD":          "",
	})
	f.env.Git.Variables = map[string]string{"GIT_CONFIG_GLOBAL": f.path("missing")}
	assertIgnored(t, f.ignored(t, "repo", "default"), "default")
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
	assertIgnored(t, f.ignoredIn(t, "wt", true, "build", "kept"), "build")
}

func TestInvalidateAllRereadsEveryGitFile(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"home/.gitconfig": "[core]\n\texcludesFile = ~/first\n",
		"home/first":      "first\n",
		"home/second":     "second\n",
		"repo/.git/HEAD":  "",
	})
	ignores := NewIgnores(f.env, os.ReadFile, func() time.Time { return time.Time{} }, time.Hour)
	repo := Repository{Top: f.path("repo")}
	assertIgnored(t, ignoredBy(t, ignores, repo, "first", "second"), "first")

	writeFiles(t, f.dir, map[string]string{"home/.gitconfig": "[core]\n\texcludesFile = ~/second\n"})
	assertIgnored(t, ignoredBy(t, ignores, repo, "first", "second"), "first")
	ignores.InvalidateAll()
	assertIgnored(t, ignoredBy(t, ignores, repo, "first", "second"), "second")
}
