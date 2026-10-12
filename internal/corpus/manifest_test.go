package corpus

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const commit = "0123456789abcdef0123456789abcdef01234567"

func TestParseManifestReadsEveryField(t *testing.T) {
	t.Parallel()

	text := "\nnvm scripts MIT https://github.com/nvm-sh/nvm " + commit + "\n" +
		"k8s scripts Apache-2.0 https://github.com/kubernetes/kubernetes " + commit + " hack cluster/addons\n"
	repos, err := ParseManifest(text)
	if err != nil {
		t.Fatal(err)
	}
	want := []Repo{
		{Name: "nvm", Kind: KindScripts, License: "MIT", URL: "https://github.com/nvm-sh/nvm", Commit: commit, Paths: []string{}},
		{Name: "k8s", Kind: KindScripts, License: "Apache-2.0", URL: "https://github.com/kubernetes/kubernetes", Commit: commit, Paths: []string{"hack", "cluster/addons"}},
	}
	if len(repos) != len(want) {
		t.Fatalf("got %d repositories, want %d", len(repos), len(want))
	}
	for i := range want {
		got := repos[i]
		if got.Name != want[i].Name || got.Kind != want[i].Kind || got.License != want[i].License ||
			got.URL != want[i].URL || got.Commit != want[i].Commit || !slices.Equal(got.Paths, want[i].Paths) {
			t.Errorf("repository %d = %+v, want %+v", i, got, want[i])
		}
	}
}

func TestParseManifestRejectsInvalidLines(t *testing.T) {
	t.Parallel()

	valid := "MIT https://github.com/a/b " + commit
	for _, tc := range []struct{ text, want string }{
		{"", "no repositories"},
		{"Bad scripts " + valid, "repository name"},
		{"a tarball " + valid, "kind"},
		{"a scripts MIT http://github.com/a/b " + commit, "URL"},
		{"a scripts MIT https://gitlab.com/a/b " + commit, "URL"},
		{"a scripts MIT https://github.com/a/b " + strings.ToUpper(commit), "commit"},
		{"a scripts MIT https://github.com/a/b abc123", "commit"},
		{"a scripts M/T https://github.com/a/b " + commit, "licence"},
		{"a scripts " + valid + " ../up", "path"},
		{"a scripts " + valid + " /abs", "path"},
		{"a scripts " + valid + " src/*.sh", "path"},
		{"a scripts " + valid + " a//b", "path"},
		{"a scripts MIT https://github.com/a/b", "want name"},
		{"a scripts " + valid + "\na scripts " + valid, "used twice"},
	} {
		if _, err := ParseManifest(tc.text); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ParseManifest(%q) = %v, want an error mentioning %q", tc.text, err, tc.want)
		}
	}
}

func TestRepoDirChangesWithWhatIsFetched(t *testing.T) {
	t.Parallel()

	base := Repo{Name: "a", URL: "https://github.com/a/b", Commit: commit}
	moved := base
	moved.Commit = strings.Repeat("f", 40)
	narrowed := base
	narrowed.Paths = []string{"src"}
	dirs := map[string]bool{}
	for _, r := range []Repo{base, moved, narrowed} {
		dir := r.Dir("cache")
		if filepath.Dir(dir) != "cache" || !strings.HasPrefix(filepath.Base(dir), "a@") {
			t.Errorf("Dir = %s, want cache/a@KEY", dir)
		}
		dirs[dir] = true
	}
	if len(dirs) != 3 {
		t.Errorf("a new commit or new paths kept the cache directory: %v", dirs)
	}
}

func TestPruneRemovesOnlyStaleCorpusEntries(t *testing.T) {
	t.Parallel()

	cache := t.TempDir()
	kept := Repo{Name: "kept", URL: "https://github.com/a/kept", Commit: commit}
	stale := Repo{Name: "stale", URL: "https://github.com/a/stale", Commit: commit}
	for _, dir := range []string{kept.Dir(cache), stale.Dir(cache), filepath.Join(cache, ".fetch-kept-123"), filepath.Join(cache, "unrelated")} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := prune([]Repo{kept}, cache); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(cache)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if want := []string{filepath.Base(kept.Dir(cache)), "unrelated"}; !slices.Equal(names, want) {
		t.Errorf("cache holds %v after pruning, want %v", names, want)
	}
	if err := Fetched([]Repo{kept}, cache); err != nil {
		t.Errorf("Fetched reports the kept repository missing: %v", err)
	}
	if err := Fetched([]Repo{kept, stale}, cache); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Errorf("Fetched = %v, want an error naming the pruned repository", err)
	}
}
