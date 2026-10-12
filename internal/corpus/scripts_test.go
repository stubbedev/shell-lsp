package corpus

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

func TestClassifyKeepsOnlyScriptsThatNameTheirShell(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, text string
		want       dialect.Dialect
		ok         bool
	}{
		{"r/a.sh", "echo hi\n", dialect.Posix, true},
		{"r/bin/tool", "#!/usr/bin/env bash\necho hi\n", dialect.Bash, true},
		{"r/_git", "#compdef git\n_arguments\n", dialect.Zsh, true},
		{"r/lib/x", "# shellcheck shell=dash\necho hi\n", dialect.Dash, true},
		{"r/run.sh", "#!/usr/bin/env python3\nprint()\n", 0, false},
		{"r/README", "echo hi\n", 0, false},
		{"r/t/a.bats", "#!/usr/bin/env bats\n@test x { :; }\n", 0, false},
	} {
		got, ok := Classify(tc.name, tc.text)
		if ok != tc.ok || ok && got.Dialect != tc.want {
			t.Errorf("Classify(%s) = %s, %v; want %s, %v", tc.name, got.Dialect, ok, tc.want, tc.ok)
		}
	}
}

func TestScriptsListsEachRepositoryUnderItsName(t *testing.T) {
	t.Parallel()

	cache := t.TempDir()
	repo := Repo{Name: "demo", Kind: KindScripts, URL: "https://github.com/a/demo", Commit: commit}
	spec := Repo{Name: "spec", Kind: KindOilsSpec, URL: "https://github.com/a/spec", Commit: commit}
	files := map[string]string{
		filepath.Join(repo.Dir(cache), "b.sh"):          "echo b\n",
		filepath.Join(repo.Dir(cache), "sub", "a.bash"): "echo a\n",
		filepath.Join(repo.Dir(cache), "crlf.sh"):       "echo a\r\n",
		filepath.Join(repo.Dir(cache), "notes.txt"):     "echo a\n",
		filepath.Join(spec.Dir(cache), "x.test.sh"):     "#### one\necho 1\n",
	}
	for path, text := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	listing, err := Scripts([]Repo{repo, spec}, cache)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, s := range listing.Scripts {
		paths = append(paths, s.Path)
	}
	if len(paths) != 2 || paths[0] != "demo/b.sh" || paths[1] != "demo/sub/a.bash" {
		t.Errorf("scripts = %v, want demo/b.sh and demo/sub/a.bash", paths)
	}
	if listing.Skipped != 1 {
		t.Errorf("skipped %d scripts, want the CRLF one", listing.Skipped)
	}
	cases, err := SpecCases([]Repo{repo, spec}, cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 1 || cases[0].ID() != "spec/x.test.sh#1" || cases[0].Code != "echo 1\n\n" {
		t.Errorf("spec cases = %+v", cases)
	}
}
