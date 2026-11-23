package gitrepo

import (
	"maps"
	"os"
	"path/filepath"
	"testing"
)

func readTextFile(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	return string(data), err == nil, err
}

func TestResolveRefFollowsSymbolicRefsIntoOtherWorktrees(t *testing.T) {
	t.Parallel()

	const mainID, sideID, packedID = "1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222", "3333333333333333333333333333333333333333"
	files := repoFiles("main")
	maps.Copy(files, map[string]string{
		"main/.git/refs/heads/main":                 mainID + "\n",
		"main/.git/refs/heads/side":                 sideID + "\n",
		"main/.git/refs/heads/to-main":              "ref: main-worktree/HEAD\n",
		"main/.git/refs/heads/to-other":             "ref: worktrees/other/HEAD\n",
		"main/.git/refs/heads/to-bisect":            "ref: worktrees/other/refs/bisect/bad\n",
		"main/.git/refs/heads/to-packed":            "ref: refs/heads/packed\n",
		"main/.git/refs/heads/dir/inner":            mainID + "\n",
		"main/.git/packed-refs":                     "# pack-refs with: peeled fully-peeled sorted\n" + packedID + " refs/heads/packed\n^" + mainID + "\n",
		"main/.git/worktrees/wt/HEAD":               "ref: refs/heads/to-main\n",
		"main/.git/worktrees/wt/commondir":          "../..\n",
		"main/.git/worktrees/other/HEAD":            "ref: refs/heads/side\n",
		"main/.git/worktrees/other/refs/bisect/bad": sideID + "\n",
		"wt/.git": "gitdir: ../main/.git/worktrees/wt\n",
	})
	f := newFixture(t, files)
	repo, found, err := Discover(f.path("wt"))
	if err != nil || !found {
		t.Fatalf("Discover(wt) = %v, %v", found, err)
	}
	for _, tt := range []struct {
		name, ref, final, id string
	}{
		{"the main worktree's HEAD", "refs/heads/to-main", "refs/heads/main", mainID},
		{"another worktree's HEAD", "refs/heads/to-other", "refs/heads/side", sideID},
		{"another worktree's per-worktree ref", "refs/heads/to-bisect", "worktrees/other/refs/bisect/bad", sideID},
		{"a packed ref", "refs/heads/to-packed", "refs/heads/packed", packedID},
		{"this worktree's HEAD", "HEAD", "refs/heads/main", mainID},
		{"a directory where a ref would be", "refs/heads/dir", "refs/heads/dir", ""},
		{"an unborn branch", "refs/heads/unborn", "refs/heads/unborn", ""},
	} {
		ref, resolved, err := repo.ResolveRef(readTextFile, tt.ref)
		if err != nil || !resolved || ref.Name != tt.final {
			t.Errorf("%s: ResolveRef = %+v, %v, %v; want %s", tt.name, ref, resolved, err, tt.final)
			continue
		}
		id, stored, err := repo.ObjectID(readTextFile, ref)
		if err != nil || id != tt.id || stored != (tt.id != "") {
			t.Errorf("%s: ObjectID = %q, %v, %v; want %q", tt.name, id, stored, err, tt.id)
		}
	}
}

func TestResolveRefTakesASymlinkToARefNameForASymbolicRef(t *testing.T) {
	t.Parallel()

	const mainID, sideID = "1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222"
	files := repoFiles("repo")
	maps.Copy(files, map[string]string{
		"repo/.git/refs/heads/main": mainID + "\n",
		"repo/.git/refs/heads/side": sideID + "\n",
	})
	f := newFixture(t, files)
	heads := f.path("repo/.git/refs/heads")
	symlink(t, "refs/heads/side", filepath.Join(heads, "named"))
	symlink(t, filepath.Join("..", "..", "refs", "heads", "side"), filepath.Join(heads, "pathed"))
	symlink(t, "refs/heads/side..x", filepath.Join(heads, "invalid"))
	symlink(t, "refs/heads/unborn", filepath.Join(heads, "dangling"))
	repo, found, err := Discover(f.path("repo"))
	if err != nil || !found {
		t.Fatalf("Discover(repo) = %v, %v", found, err)
	}
	for _, tt := range []struct {
		name, ref, final, id string
	}{
		{"a link naming a ref", "refs/heads/named", "refs/heads/side", sideID},
		{"a link naming a ref that is not there", "refs/heads/dangling", "refs/heads/unborn", ""},
		{"a link naming a path", "refs/heads/pathed", "refs/heads/pathed", sideID},
		{"a link naming no valid ref", "refs/heads/invalid", "refs/heads/invalid", ""},
	} {
		ref, resolved, err := repo.ResolveRef(readTextFile, tt.ref)
		if err != nil || !resolved || ref.Name != tt.final {
			t.Errorf("%s: ResolveRef = %+v, %v, %v; want %s", tt.name, ref, resolved, err, tt.final)
			continue
		}
		id, stored, err := repo.ObjectID(readTextFile, ref)
		if err != nil || id != tt.id || stored != (tt.id != "") {
			t.Errorf("%s: ObjectID = %q, %v, %v; want %q", tt.name, id, stored, err, tt.id)
		}
	}
}
