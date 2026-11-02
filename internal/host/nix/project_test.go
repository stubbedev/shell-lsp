package nix

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host/model"
)

const lockedInputs = `{
  "nodes": {
    "nixpkgs": {"locked": {"narHash": "sha256-VVTPf+Hyd5ebpjBMHmrLMSBIeW6ls48Bqtosj7CNKLA="}},
    "parts": {"inputs": {"nixpkgs-lib": ["nixpkgs"]}, "locked": {"narHash": "sha256-VVTPf+Hyd5ebpjBMHmrLMSBIeW6ls48Bqtosj7CNKLA=", "dir": "sub"}},
    "root": {"inputs": {"nixpkgs": "nixpkgs", "parts": "parts", "alias": ["parts", "nixpkgs-lib"]}}
  },
  "root": "root",
  "version": 7
}`

const lockedStore = "/nix/store/2s013d7g7sna6rls7nk0yy8343h6mdiw-source"

func TestStorePathOfALockedInput(t *testing.T) {
	t.Parallel()

	lock, ok := parseFlakeLock([]byte(lockedInputs))
	if !ok {
		t.Fatal("the lock does not parse")
	}
	for input, want := range map[string]string{
		"nixpkgs": lockedStore,
		"parts":   lockedStore + "/sub",
		"alias":   lockedStore,
	} {
		if got, found := lock.sourceOf(input); !found || got != want {
			t.Errorf("source of %s = %q, %v, want %q", input, got, found, want)
		}
	}
	if got, found := lock.sourceOf("absent"); found {
		t.Errorf("an input that is not locked has source %q", got)
	}
	for _, narHash := range []string{"", "md5-abc", "sha256-not base64"} {
		if got, found := storePath(narHash); found {
			t.Errorf("hash %q has store path %q", narHash, got)
		}
	}
	for _, bad := range []string{"", "[]", `{"root": "gone", "nodes": {}}`} {
		if _, ok := parseFlakeLock([]byte(bad)); ok {
			t.Errorf("lock %q parses", bad)
		}
	}
}

func TestImportTreeRule(t *testing.T) {
	t.Parallel()

	root := "/w/_root/modules"
	got := keptModules(root, []string{
		root + "/a.nix",
		root + "/_b.nix",
		root + "/c/d.nix",
		root + "/_e/f.nix",
		root + "/g/_h/i.nix",
		root + "/j.txt",
		root + "/k_l.nix",
	})
	if want := []string{root + "/a.nix", root + "/c/d.nix", root + "/k_l.nix"}; !slices.Equal(got, want) {
		t.Errorf("kept %q, want %q", got, want)
	}
}

type memorySource map[string]string

func (m memorySource) LoadAll(paths []string) []*model.Index {
	out := make([]*model.Index, len(paths))
	for i, path := range paths {
		if text, ok := m[path]; ok {
			out[i] = Analyze(text).Index
		}
	}
	return out
}

func (m memorySource) Tree(string) ([]string, error) { return nil, nil }

func (m memorySource) Read(path string) ([]byte, error) {
	if text, ok := m[path]; ok {
		return []byte(text), nil
	}
	return nil, errors.New(path + ": " + fs.ErrNotExist.Error())
}

func symbolNamed(t *testing.T, ix *model.Index, name string) int {
	t.Helper()
	i := slices.IndexFunc(ix.Symbols, func(s model.Symbol) bool { return s.Name == name })
	if i < 0 {
		t.Fatalf("no symbol %q", name)
	}
	return i
}

func TestLockedInputsLeadIntoTheStore(t *testing.T) {
	t.Parallel()

	flake := filepath.Join("/w", FlakeFile)
	text := "{\n  inputs.nixpkgs.url = \"github:NixOS/nixpkgs\";\n  inputs.unlocked.url = \"github:example/none\";\n  outputs = _: { };\n}\n"
	self := Analyze(text).Index
	source := memorySource{filepath.Join("/w", lockFile): lockedInputs}
	p := Link(t.Context(), Request{Self: self, SelfPath: flake, Flake: flake}, source)
	want := []string{filepath.Join(lockedStore, FlakeFile), filepath.Join(lockedStore, DirectoryEntry)}
	if got := p.Sources(0, symbolNamed(t, self, "nixpkgs")); !slices.Equal(got, want) {
		t.Errorf("sources of a locked input = %q, want %q", got, want)
	}
	if got := p.Sources(0, symbolNamed(t, self, "unlocked")); got != nil {
		t.Errorf("sources of an input the lock does not know = %q", got)
	}
	if got := p.Sources(0, symbolNamed(t, self, "url")); got != nil {
		t.Errorf("sources of an attribute that is no input = %q", got)
	}
	bare := Link(t.Context(), Request{Self: self, SelfPath: flake, Flake: flake}, memorySource{})
	if got := bare.Sources(0, symbolNamed(t, self, "nixpkgs")); got != nil {
		t.Errorf("sources without a lock = %q", got)
	}
}

func TestLinkStopsWhenCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	p := Link(ctx, Request{Self: Analyze("{ a = 1; }\n").Index}, localSource{})
	if reason := p.incomplete(); !strings.Contains(reason, context.Canceled.Error()) {
		t.Errorf("incomplete = %q, want it to name the cancellation", reason)
	}
}
