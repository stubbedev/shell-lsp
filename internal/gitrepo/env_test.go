package gitrepo

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/stubbedev/shell-lsp/internal/platform"
)

func TestCurrentProcessKeepsOnlyTheGitVariablesItReads(t *testing.T) {
	t.Parallel()

	p := CurrentProcess([]string{"HOME=/h", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=a.b", "GIT_DIR=/x", "GIT_INDEX_FILE=/i", "GIT_CONFIG_GLOBAL="}, nil, nil)
	want := map[string]string{"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "a.b", "GIT_DIR": "/x", "GIT_CONFIG_GLOBAL": ""}
	if diff := cmp.Diff(want, p.Variables); diff != "" {
		t.Errorf("Variables (-want +got):\n%s", diff)
	}
	if _, err := p.Prefix(); err == nil {
		t.Error("a prefix was found without a PATH")
	}
}

func executable(t *testing.T, dir string) []string {
	t.Helper()
	name, pathExt := executableName, []string(nil)
	if platform.Native == platform.Windows {
		name, pathExt = executableName+".exe", []string{".exe"}
	}
	writeFiles(t, dir, map[string]string{name: ""})
	if err := os.Chmod(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	return pathExt
}

func TestInstallPrefixIsTheParentOfTheBinDirectoryHoldingGit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	prefix := filepath.Join(root, "prefix")
	pathExt := executable(t, filepath.Join(prefix, "bin"))
	got, err := installPrefix([]string{filepath.Join(root, "empty"), filepath.Join(prefix, "bin")}, pathExt)
	real, realErr := filepath.EvalSymlinks(prefix)
	if err != nil || realErr != nil || got != real {
		t.Errorf("installPrefix = %q, %v; want %q (%v)", got, err, real, realErr)
	}

	executable(t, filepath.Join(root, "loose"))
	if got, err := installPrefix([]string{filepath.Join(root, "loose")}, pathExt); err == nil {
		t.Errorf("installPrefix of a git outside a bin directory = %q, want an error", got)
	}
}

func TestInstallPrefixFollowsALinkToGit(t *testing.T) {
	t.Parallel()

	if platform.Native == platform.Windows {
		t.Skip("git for Windows is not installed through symbolic links")
	}
	root := t.TempDir()
	executable(t, filepath.Join(root, "store", "git", "bin"))
	writeFiles(t, root, map[string]string{"profile/keep": ""})
	if err := os.Symlink(filepath.Join(root, "store", "git", "bin", executableName), filepath.Join(root, "profile", executableName)); err != nil {
		t.Fatal(err)
	}
	got, err := installPrefix([]string{filepath.Join(root, "profile")}, nil)
	want, realErr := filepath.EvalSymlinks(filepath.Join(root, "store", "git"))
	if err != nil || realErr != nil || got != want {
		t.Errorf("installPrefix = %q, %v; want %q (%v)", got, err, want, realErr)
	}
}

func TestUserHomeLeavesAnUnknownUserUnexpanded(t *testing.T) {
	t.Parallel()

	if _, err := userHome("shell-lsp-no-such-user"); !errors.Is(err, errNotExpanded) {
		t.Errorf("userHome of an unknown user = %v, want %v", err, errNotExpanded)
	}
}

func TestBaseLeavesAGitdirPatternGitCannotExpandAsWritten(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"home/.gitconfig":    "[includeIf \"gitdir:~nobody/\"]\n\tpath = never\n",
		"home/never":         "[core]\n\texcludesFile = ~/never-ignore\n",
		"home/never-ignore":  "never\n",
		"repo/.git/HEAD":     "ref: refs/heads/main\n",
		"repo/.git/objects/": "",
		"repo/.git/refs/":    "",
	})
	f.env.Git.UserHome = func(string) (string, error) { return "", errNotExpanded }
	assertIgnored(t, f.ignored(t, "repo", "never"))

	f.env.Git.UserHome = func(string) (string, error) { return f.path("repo"), nil }
	assertIgnored(t, f.ignored(t, "repo", "never"), "never")
}
