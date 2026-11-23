package workspace

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/gitrepo"
	"github.com/stubbedev/shell-lsp/internal/platform"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTestWorkspace(c *clock) *Workspace {
	ws := New(func(string) (*File, bool) { return nil, false }, Detect, gitrepo.Environment{})
	ws.now = c.now
	return ws
}

func rewrite(t *testing.T, path, text string, at time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func firstFunction(t *testing.T, ws *Workspace, path string) string {
	t.Helper()
	f, err := ws.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return f.Tree.Functions[0].Name
}

func TestUnwatchedFilesRevalidateAfterTheRecheckInterval(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "a.sh")
	c := &clock{t: time.Now()}
	ws := newTestWorkspace(c)
	rewrite(t, path, "one() { :; }\n", c.t)
	if got := firstFunction(t, ws, path); got != "one" {
		t.Fatalf("first load = %s", got)
	}
	rewrite(t, path, "two() { :; }\n", c.t.Add(time.Hour))
	c.t = c.t.Add(DiskRecheck / 2)
	if got := firstFunction(t, ws, path); got != "one" {
		t.Errorf("load within the recheck interval = %s, want the cached one", got)
	}
	c.t = c.t.Add(DiskRecheck)
	if got := firstFunction(t, ws, path); got != "two" {
		t.Errorf("load after the recheck interval = %s, want the changed file", got)
	}
}

func TestWatchedEntriesSkipValidationUntilInvalidated(t *testing.T) {
	t.Parallel()

	root, outside := t.TempDir(), t.TempDir()
	inside, external := filepath.Join(root, "a.sh"), filepath.Join(outside, "b.sh")
	c := &clock{t: time.Now()}
	ws := newTestWorkspace(c)
	ws.SetRoots([]string{root})
	ws.SetWatched(true)
	rewrite(t, inside, "one() { :; }\n", c.t)
	rewrite(t, external, "one() { :; }\n", c.t)
	firstFunction(t, ws, inside)
	firstFunction(t, ws, external)
	rewrite(t, inside, "two() { :; }\n", c.t.Add(time.Hour))
	rewrite(t, external, "two() { :; }\n", c.t.Add(time.Hour))
	c.t = c.t.Add(time.Hour)
	if got := firstFunction(t, ws, inside); got != "one" {
		t.Errorf("watched file = %s, want the cached one until an event invalidates it", got)
	}
	if got := firstFunction(t, ws, external); got != "two" {
		t.Errorf("file outside the watched roots = %s, want it revalidated", got)
	}
	generation := ws.Generation()
	ws.Invalidate(inside)
	if ws.Generation() == generation {
		t.Error("Invalidate did not advance the generation")
	}
	if got := firstFunction(t, ws, inside); got != "two" {
		t.Errorf("watched file after invalidation = %s", got)
	}
}

func TestFileMemoFollowsGeneration(t *testing.T) {
	t.Parallel()

	f := &File{}
	calls := 0
	compute := func() int { calls++; return calls }
	first := Memo(f, "k", 1, compute)
	again := Memo(f, "k", 1, compute)
	next := Memo(f, "k", 2, compute)
	if first != 1 || again != 1 || next != 2 {
		t.Errorf("memo results %v %v %v, want 1 1 2", first, again, next)
	}
}

func TestFileMemoComputesOnceForConcurrentCallers(t *testing.T) {
	t.Parallel()

	f := &File{}
	var calls atomic.Int32
	release := make(chan struct{})
	compute := func() string {
		calls.Add(1)
		<-release
		return "result"
	}
	var wg sync.WaitGroup
	results := make([]string, 8)
	for i := range results {
		wg.Go(func() { results[i] = Memo(f, "k", 1, compute) })
	}
	for calls.Load() == 0 {
		runtime.Gosched()
	}
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Errorf("computed %d times, want once", calls.Load())
	}
	for i, r := range results {
		if r != "result" {
			t.Errorf("caller %d got %v", i, r)
		}
	}
}

func TestFileMemoRecomputesAfterAPanic(t *testing.T) {
	t.Parallel()

	f := &File{}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("the panic did not reach the computing caller")
			}
		}()
		Memo(f, "k", 1, func() string { panic("boom") })
	}()
	if got := Memo(f, "k", 1, func() string { return "recovered" }); got != "recovered" {
		t.Errorf("after a panic the memo returned %v, want a fresh computation", got)
	}
}

func TestMemoRejectsAKeyReusedForAnotherType(t *testing.T) {
	t.Parallel()

	f := &File{URI: "file:///a.sh"}
	Memo(f, "k", 1, func() int { return 1 })
	defer func() {
		if recover() == nil {
			t.Error("reading an int memo as a string did not fail")
		}
	}()
	Memo(f, "k", 1, func() string { return "s" })
}

func TestShellFilesCacheFollowsDirectoryChanges(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, dir := range []string{"sub", "node_modules", "sub/deep"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"a.sh", "notes.txt", "node_modules/x.sh", "sub/deep/b.bash", "sub/.zshrc"} {
		if err := os.WriteFile(filepath.Join(root, f), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ws := New(func(string) (*File, bool) { return nil, false }, Detect, gitrepo.Environment{})
	ws.SetRoots([]string{root})
	rel := func() []string { return relShellFiles(t, ws, root) }
	if got := rel(); len(got) != 3 || got[0] != "a.sh" || got[1] != "sub/.zshrc" || got[2] != "sub/deep/b.bash" {
		t.Fatalf("ShellFiles = %v", got)
	}
	if err := os.WriteFile(filepath.Join(root, "sub/deep/c.sh"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(root, "sub/deep"), later, later); err != nil {
		t.Fatal(err)
	}
	if got := rel(); len(got) != 4 || got[3] != "sub/deep/c.sh" {
		t.Errorf("ShellFiles after adding a file = %v", got)
	}
}

func TestSourcedShellFilesAreFoundAndDetectedByName(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	want := map[string]dialect.Dialect{".envrc": dialect.Bash, "x.zsh-theme": dialect.Zsh}
	for name := range want {
		if err := os.WriteFile(filepath.Join(root, name), []byte("export A=1\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ws := New(func(string) (*File, bool) { return nil, false }, Detect, gitrepo.Environment{})
	ws.SetRoots([]string{root})
	if got := relShellFiles(t, ws, root); !slices.Equal(got, []string{".envrc", "x.zsh-theme"}) {
		t.Errorf("ShellFiles = %v, want .envrc and x.zsh-theme", got)
	}
	for name, d := range want {
		path := filepath.Join(root, name)
		if !ShellByName(path) {
			t.Errorf("ShellByName(%q) = false, want true", name)
		}
		f, err := ws.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if f.Detection.Dialect != d {
			t.Errorf("%s is detected as %v, want %v", name, f.Detection.Dialect, d)
		}
	}
	globs := WatchGlobs()
	if !strings.Contains(globs[0], ",zsh-theme,") || !strings.Contains(globs[1], ",envrc,") {
		t.Errorf("WatchGlobs = %q, want zsh-theme and envrc", globs)
	}
}

func TestRootOfPicksTheInnermostRoot(t *testing.T) {
	t.Parallel()

	ws := New(nil, nil, gitrepo.Environment{})
	ws.SetRoots([]string{filepath.FromSlash("/a"), filepath.FromSlash("/a/b"), filepath.FromSlash("/c")})
	for path, want := range map[string]string{"/a/b/x.bats": "/a/b", "/a/x.bats": "/a", "/c/d/e.bats": "/c", "/z/x.bats": ""} {
		path, want = filepath.FromSlash(path), filepath.FromSlash(want)
		if got, ok := ws.RootOf(path); got != want || ok != (want != "") {
			t.Errorf("RootOf(%q) = %q, %t, want %q", path, got, ok, want)
		}
	}
}

func TestLoadForgetsFilesDeletedFromDisk(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "a.sh")
	c := &clock{t: time.Now()}
	ws := newTestWorkspace(c)
	rewrite(t, path, "one() { :; }\n", c.t)
	firstFunction(t, ws, path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	c.t = c.t.Add(DiskRecheck)
	if _, err := ws.Load(path); err == nil {
		t.Fatal("loading a deleted file succeeded")
	}
	if len(ws.files) != 0 {
		t.Errorf("cache kept the deleted file: %v", ws.files)
	}
}

func TestMissingFilesAreRecheckedOnlyAfterTheIntervalOrAnInvalidation(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "lib.sh")
	c := &clock{t: time.Now()}
	ws := newTestWorkspace(c)
	if _, err := ws.Load(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("loading a missing file = %v, want not-exist", err)
	}
	rewrite(t, path, "one() { :; }\n", c.t)
	c.t = c.t.Add(DiskRecheck / 2)
	if _, err := ws.Load(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("load within the recheck interval = %v, want the remembered miss", err)
	}
	ws.Invalidate(path)
	if got := firstFunction(t, ws, path); got != "one" {
		t.Errorf("load after an invalidation = %s, want the created file", got)
	}
	other := filepath.Join(filepath.Dir(path), "other.sh")
	if _, err := ws.Load(other); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("loading a missing file = %v, want not-exist", err)
	}
	rewrite(t, other, "two() { :; }\n", c.t)
	c.t = c.t.Add(DiskRecheck)
	if got := firstFunction(t, ws, other); got != "two" {
		t.Errorf("load after the recheck interval = %s, want the created file", got)
	}
}

func TestMissingFileMemoryIsBounded(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c := &clock{t: time.Now()}
	ws := newTestWorkspace(c)
	for i := range maxMissing + 10 {
		_, _ = ws.Load(filepath.Join(dir, strconv.Itoa(i)+".sh"))
	}
	if len(ws.missing) > maxMissing {
		t.Errorf("remembered %d missing paths, want at most %d", len(ws.missing), maxMissing)
	}
}

func TestShellFilesForgetsRemovedDirectories(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &clock{t: time.Now()}
	ws := newTestWorkspace(c)
	ws.SetRoots([]string{root})
	for _, name := range []string{"a.sh", "sub/b.sh", "sub/deep/c.sh"} {
		rewrite(t, filepath.Join(root, name), "f() { :; }\n", c.t)
	}
	paths, err := ws.ShellFiles(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		firstFunction(t, ws, path)
	}
	if err := os.RemoveAll(filepath.Join(root, "sub")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "a.sh")); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(root, later, later); err != nil {
		t.Fatal(err)
	}
	if paths, err := ws.ShellFiles(t.Context()); err != nil || len(paths) != 0 {
		t.Fatalf("ShellFiles after removing everything = %v, %v", paths, err)
	}
	if len(ws.dirs) != 1 || len(ws.files) != 0 {
		t.Errorf("cache kept removed entries: dirs %v, files %v", ws.dirs, ws.files)
	}
}

func TestMakefilesLoadAsTheShellOfTheirRecipes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"Makefile":         "SHELL := bash\nall:\n\tgreet() { echo hi; }; greet\n",
		"rules/common.mk":  "x:\n\techo x\n",
		"Makefile.am":      "bin_PROGRAMS = x\n",
		"scripts/run.sh":   "echo run\n",
		"notes/README.txt": "text\n",
	})
	ws := New(func(string) (*File, bool) { return nil, false }, func(string, dialect.Input) dialect.Detection {
		t.Error("a Makefile asked for the dialect of a shell script")
		return dialect.Detection{Dialect: dialect.Bash}
	}, gitrepo.Environment{})
	ws.SetRoots([]string{root})
	paths, err := ws.AllShellFiles(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(root, "Makefile"), filepath.Join(root, "rules", "common.mk"), filepath.Join(root, "scripts", "run.sh")}
	if !slices.Equal(paths, want) {
		t.Errorf("shell files = %v, want %v", paths, want)
	}
	f, err := ws.Load(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	if !f.Doc.Projected() || f.Doc.Source().Text() != "SHELL := bash\nall:\n\tgreet() { echo hi; }; greet\n" || f.Tree.Dialect != dialect.Bash {
		t.Errorf("Makefile loaded as %q in %v", f.Doc.Text(), f.Tree.Dialect)
	}
	if len(f.Tree.Functions) != 1 || f.Tree.Functions[0].Name != "greet" {
		t.Errorf("functions = %+v", f.Tree.Functions)
	}
	if globs := WatchGlobs(); !strings.Contains(globs[2], "{Makefile,makefile,GNUmakefile,") || !strings.Contains(globs[0], ",mk,mak") {
		t.Errorf("watch globs = %v", globs)
	}
}

func relShellFiles(t *testing.T, ws *Workspace, base string) []string {
	t.Helper()
	paths, err := ws.ShellFiles(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(paths))
	for i, p := range paths {
		rel, err := filepath.Rel(base, p)
		if err != nil {
			t.Fatal(err)
		}
		out[i] = filepath.ToSlash(rel)
	}
	return out
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, text := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestShellFilesSkipWhatGitIgnores(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	writeTree(t, repo, map[string]string{
		".git/info/exclude":        "/local.sh\n",
		".git/HEAD":                "ref: refs/heads/main\n",
		".git/objects/info/packs":  "",
		".git/refs/heads/main":     "0123456789abcdef0123456789abcdef01234567\n",
		".gitignore":               ".devenv*\n**/target/**\n*.gen.sh\n!keep.gen.sh\n",
		"a.sh":                     "",
		"local.sh":                 "",
		"x.gen.sh":                 "",
		"keep.gen.sh":              "",
		".devenv/shell-1.sh":       "",
		"target/debug/build.sh":    "",
		"lib/b.sh":                 "",
		"lib/.gitignore":           "/private.sh\n!y.gen.sh\n",
		"lib/private.sh":           "",
		"lib/y.gen.sh":             "",
		"lib/deep/private.sh":      "",
		"lib/deep/target/inner.sh": "",
	})
	ws := New(func(string) (*File, bool) { return nil, false }, Detect, gitrepo.Environment{})
	ws.SetRoots([]string{repo})
	want := []string{"a.sh", "keep.gen.sh", "lib/b.sh", "lib/deep/private.sh", "lib/y.gen.sh"}
	if got := relShellFiles(t, ws, repo); !slices.Equal(got, want) {
		t.Errorf("ShellFiles = %v, want %v", got, want)
	}

	ws.SetRoots([]string{filepath.Join(repo, "lib")})
	want = []string{"lib/b.sh", "lib/deep/private.sh", "lib/y.gen.sh"}
	if got := relShellFiles(t, ws, repo); !slices.Equal(got, want) {
		t.Errorf("ShellFiles of a root inside the repository = %v, want %v", got, want)
	}
}

func TestShellFilesSkipWhatASymlinkedGitDirectoryExcludes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"store/git/HEAD":               "ref: refs/heads/main\n",
		"store/git/objects/info/packs": "",
		"store/git/refs/heads/main":    "0123456789abcdef0123456789abcdef01234567\n",
		"store/git/info/exclude":       "/local.sh\n",
		"repo/a.sh":                    "",
		"repo/local.sh":                "",
	})
	if err := os.Symlink(filepath.Join(dir, "store", "git"), filepath.Join(dir, "repo", ".git")); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}
	ws := New(func(string) (*File, bool) { return nil, false }, Detect, gitrepo.Environment{})
	ws.SetRoots([]string{filepath.Join(dir, "repo")})
	if got := relShellFiles(t, ws, filepath.Join(dir, "repo")); !slices.Equal(got, []string{"a.sh"}) {
		t.Errorf("ShellFiles = %v, want [a.sh]", got)
	}
}

func TestShellFilesApplyTheRulesOfTheRepositoryGitFindsThroughSymlinks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	head := map[string]string{"/.git/HEAD": "ref: refs/heads/main\n", "/.git/objects/info/packs": "", "/.git/refs/heads/main": "0123456789abcdef0123456789abcdef01234567\n"}
	files := map[string]string{
		"a/.gitignore":     "/sub/shown.sh\n",
		"b/.gitignore":     "/x/hidden.sh\n/lib/private.sh\n",
		"b/x/shown.sh":     "",
		"b/x/hidden.sh":    "",
		"b/lib/b.sh":       "",
		"b/lib/private.sh": "",
		"links/keep":       "",
	}
	for _, repo := range []string{"a", "b"} {
		for name, text := range head {
			files[repo+name] = text
		}
	}
	writeTree(t, dir, files)
	for link, target := range map[string]string{"a/sub": "b/x", "links/x": "b/x", "via": "b"} {
		if err := os.Symlink(filepath.Join(dir, filepath.FromSlash(target)), filepath.Join(dir, filepath.FromSlash(link))); err != nil {
			t.Skipf("symbolic links are unavailable: %v", err)
		}
	}
	for _, tt := range []struct {
		name, root string
		want       []string
	}{
		{"a symlink into another repository", "a/sub", []string{"shown.sh"}},
		{"a symlink from outside any repository", "links/x", []string{"shown.sh"}},
		{"a repository through a symlinked parent", "via/lib", []string{"b.sh"}},
	} {
		root := filepath.Join(dir, filepath.FromSlash(tt.root))
		ws := New(func(string) (*File, bool) { return nil, false }, Detect, gitrepo.Environment{})
		ws.SetRoots([]string{root})
		if got := relShellFiles(t, ws, root); !slices.Equal(got, tt.want) {
			t.Errorf("%s: ShellFiles = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestShellFilesFollowGitignoreEdits(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTree(t, root, map[string]string{".gitignore": "", "a.sh": "", "b.sh": ""})
	ws := New(func(string) (*File, bool) { return nil, false }, Detect, gitrepo.Environment{})
	ws.SetRoots([]string{root})
	if got := relShellFiles(t, ws, root); !slices.Equal(got, []string{"a.sh", "b.sh"}) {
		t.Fatalf("ShellFiles = %v", got)
	}
	ignore := filepath.Join(root, ".gitignore")
	rewrite(t, ignore, "b.sh\n", time.Now().Add(time.Hour))
	if got := relShellFiles(t, ws, root); !slices.Equal(got, []string{"a.sh"}) {
		t.Errorf("ShellFiles after ignoring b.sh = %v", got)
	}
	if err := os.Remove(ignore); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(root, later, later); err != nil {
		t.Fatal(err)
	}
	if got := relShellFiles(t, ws, root); !slices.Equal(got, []string{"a.sh", "b.sh"}) {
		t.Errorf("ShellFiles after deleting .gitignore = %v", got)
	}
	if len(ws.ignores) != 0 {
		t.Errorf("cache kept the deleted rules: %v", ws.ignores)
	}
}

func TestShellFilesSkipWhatTheUsersExcludesFileIgnores(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	writeTree(t, base, map[string]string{
		"home/.gitconfig":              "[core]\n\texcludesFile = ~/global-ignore\n",
		"home/global-ignore":           "generated/\nvendored/\n",
		"repo/.git/HEAD":               "ref: refs/heads/main\n",
		"repo/.git/objects/info/packs": "",
		"repo/.git/refs/heads/main":    "0123456789abcdef0123456789abcdef01234567\n",
		"repo/.gitignore":              "!vendored/\n",
		"repo/a.sh":                    "",
		"repo/generated/g.sh":          "",
		"repo/vendored/v.sh":           "",
	})
	repo := filepath.Join(base, "repo")
	ws := New(func(string) (*File, bool) { return nil, false }, Detect, gitrepo.Environment{User: platform.UserDirs{Home: filepath.Join(base, "home")}})
	ws.SetRoots([]string{repo})
	if got, want := relShellFiles(t, ws, repo), []string{"a.sh", "vendored/v.sh"}; !slices.Equal(got, want) {
		t.Errorf("ShellFiles = %v, want %v", got, want)
	}
}

func TestInvalidateAllRereadsTheGitConfiguration(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	writeTree(t, base, map[string]string{
		"home/.gitconfig":              "[core]\n\texcludesFile = ~/first\n",
		"home/first":                   "first/\n",
		"home/second":                  "second/\n",
		"repo/.git/HEAD":               "ref: refs/heads/main\n",
		"repo/.git/objects/info/packs": "",
		"repo/.git/refs/heads/main":    "0123456789abcdef0123456789abcdef01234567\n",
		"repo/first/a.sh":              "",
		"repo/second/b.sh":             "",
	})
	repo := filepath.Join(base, "repo")
	ws := New(func(string) (*File, bool) { return nil, false }, Detect, gitrepo.Environment{User: platform.UserDirs{Home: filepath.Join(base, "home")}})
	frozen := time.Now()
	ws.now = func() time.Time { return frozen }
	ws.SetRoots([]string{repo})
	if got, want := relShellFiles(t, ws, repo), []string{"second/b.sh"}; !slices.Equal(got, want) {
		t.Fatalf("ShellFiles = %v, want %v", got, want)
	}
	writeTree(t, base, map[string]string{"home/.gitconfig": "[core]\n\texcludesFile = ~/second\n"})
	ws.InvalidateAll()
	if got, want := relShellFiles(t, ws, repo), []string{"first/a.sh"}; !slices.Equal(got, want) {
		t.Errorf("ShellFiles after InvalidateAll = %v, want %v", got, want)
	}
}

func TestShellFilesReadTheExcludeFileOfALinkedWorktree(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	writeTree(t, base, map[string]string{
		"main/.git/info/exclude":           "excluded.sh\n",
		"main/.git/worktrees/wt/commondir": "../..\n",
		"main/.git/worktrees/wt/HEAD":      "ref: refs/heads/wt\n",
		"main/.git/HEAD":                   "ref: refs/heads/main\n",
		"main/.git/objects/info/packs":     "",
		"main/.git/refs/heads/main":        "0123456789abcdef0123456789abcdef01234567\n",
		"wt/.git":                          "gitdir: ../main/.git/worktrees/wt\n",
		"wt/a.sh":                          "",
		"wt/excluded.sh":                   "",
	})
	worktree := filepath.Join(base, "wt")
	ws := New(func(string) (*File, bool) { return nil, false }, Detect, gitrepo.Environment{})
	ws.SetRoots([]string{worktree})
	if got := relShellFiles(t, ws, worktree); !slices.Equal(got, []string{"a.sh"}) {
		t.Errorf("ShellFiles = %v, want [a.sh]", got)
	}
}

func TestNixFilesFollowsLinksAndSkipsNothing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"a.nix", "vendor/b.nix", "_c/d.nix", "e.txt"} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "a.nix"), filepath.Join(dir, "link.nix")); err != nil {
		t.Fatal(err)
	}
	ws := New(func(string) (*File, bool) { return nil, false }, Detect, gitrepo.Environment{})
	got, err := ws.NixFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "_c", "d.nix"), filepath.Join(dir, "a.nix"), filepath.Join(dir, "link.nix"), filepath.Join(dir, "vendor", "b.nix")}
	if !slices.Equal(got, want) {
		t.Errorf("found %q, want %q", got, want)
	}
	if _, err := ws.NixFiles(filepath.Join(dir, "gone")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a missing directory: %v", err)
	}
}

func TestScanCutIsRemembered(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"a.sh", "b.sh"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("echo\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ws := New(func(string) (*File, bool) { return nil, false }, Detect, gitrepo.Environment{})
	ws.SetRoots([]string{dir})
	if _, err := ws.scan(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if !ws.ScanCut() {
		t.Error("a scan that stopped at its limit is not remembered")
	}
	if _, err := ws.ShellFiles(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ws.ScanCut() {
		t.Error("a scan that saw everything still counts as cut")
	}
}

func TestAdoptingAReopenedNixFileKeepsTheFileItWasReopenedFrom(t *testing.T) {
	path := filepath.Join(t.TempDir(), "default.nix")
	const text = "{ a = 1; }\n"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	w := New(noOverlay, Detect, gitrepo.Environment{})
	disk, err := w.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	reopened, ok := w.Reopened(path, docstore.NewDocument(fileuri.FromPath(path), "nix", 1, text))
	if !ok {
		t.Fatal("an unchanged Nix file did not reopen from the loaded one")
	}
	w.Adopt(path, reopened)
	runtime.GC()
	runtime.GC()
	got, err := w.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Native.Index != disk.Native.Index {
		t.Error("after the closed document was collected, loading parsed the unchanged file again instead of returning the file still in use")
	}
	runtime.KeepAlive(disk)
}
