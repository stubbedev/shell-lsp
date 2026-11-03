package nix

import (
	stdcontext "context"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
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
	source := memorySource{filepath.Join("/w", LockFileName): lockedInputs}
	p := Link(t.Context(), Request{Self: self, SelfPath: flake, Flake: flake}, source)
	want := []string{lockedStore + "/" + FlakeFile, lockedStore + "/" + DirectoryEntry}
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

	ctx, cancel := stdcontext.WithCancel(t.Context())
	cancel()
	p := Link(ctx, Request{Self: Analyze("{ a = 1; }\n").Index}, localSource{})
	if reason := p.incomplete(); !strings.Contains(reason, stdcontext.Canceled.Error()) {
		t.Errorf("incomplete = %q, want it to name the cancellation", reason)
	}
}

func TestRenameRefusesASharedNameADependencyDeclares(t *testing.T) {
	t.Parallel()

	const (
		flakePath = "/w/flake.nix"
		storeFile = storeDirectory + "/x-source/module.nix"
		flakeText = `{
  outputs = { self, nixpkgs }: {
    nixosConfigurations.a = nixpkgs.lib.nixosSystem {
      modules = [ ` + storeFile + ` { services.web.port = 1; } ];
    };
  };
}
`
		storeText = "{ lib, ... }: {\n  options.services.web.port = lib.mkOption { };\n}\n"
	)
	src := memorySource{flakePath: flakeText, storeFile: storeText}
	p := Link(stdcontext.Background(), Request{Flake: flakePath}, src)
	i, ok := p.FileIndex(flakePath)
	if !ok {
		t.Fatal("the flake is not in its project")
	}
	symbol := -1
	for at, sym := range p.units[i].ix.Symbols {
		if sym.Name == "port" {
			symbol = at
		}
	}
	if symbol < 0 {
		t.Fatal("the flake holds no symbol `port`")
	}
	reason, decided := p.Refusal(i, symbol)
	if !decided || !strings.Contains(reason, "dependency") {
		t.Errorf("refusal = %q, %v, want one that names the dependency", reason, decided)
	}
}

func TestDependencyFilesDoNotFollowTheirOwnImports(t *testing.T) {
	t.Parallel()

	const (
		flakePath = "/w/flake.nix"
		loaded    = storeDirectory + "/x-source/used.nix"
		unused    = storeDirectory + "/x-source/unused.nix"
	)
	src := memorySource{
		flakePath: "{ outputs = { self }: { value = import " + loaded + "; }; }\n",
		loaded:    "{ lazy = import " + unused + "; own = 1; }\n",
		unused:    "{ never = 1; }\n",
	}
	p := Link(stdcontext.Background(), Request{Flake: flakePath}, src)
	if _, ok := p.FileIndex(loaded); !ok {
		t.Error("the imported dependency file was not loaded")
	}
	if _, ok := p.FileIndex(unused); ok {
		t.Error("a file only a dependency imports was loaded without anything reading it")
	}
}

func TestFreshNoticesAnAbsentFileAppearing(t *testing.T) {
	t.Parallel()

	const flakePath = "/w/flake.nix"
	later := filepath.Join("/w", "later.nix")
	files := memorySource{flakePath: "{ outputs = { self }: { value = import ./later.nix; }; }\n"}
	src := onceSource{memorySource: files, seen: map[string]*model.Index{}}
	p := Link(stdcontext.Background(), Request{Flake: flakePath}, src)
	if !p.Fresh(src) {
		t.Fatal("a project is stale right after its link")
	}
	files[later] = "{ }\n"
	delete(src.seen, later)
	if p.Fresh(src) {
		t.Error("the project stays fresh when a file it wanted appears")
	}
}

func TestTheProjectStopsAtItsSizeBudget(t *testing.T) {
	t.Parallel()

	const flakePath = "/w/flake.nix"
	src := memorySource{}
	var imports strings.Builder
	body := "{ pad = \"" + strings.Repeat("x", 1<<20) + "\"; }\n"
	for i := range 2 * maxProjectBytes / (1 << 20) {
		name := filepath.Join("/w", "f"+strconv.Itoa(i)+".nix")
		src[name] = body
		imports.WriteString(" ./f" + strconv.Itoa(i) + ".nix")
	}
	src[flakePath] = "{ outputs = { self }: { imports = [" + imports.String() + " ]; }; }\n"
	p := Link(stdcontext.Background(), Request{Flake: flakePath}, src)
	if !p.Truncated() {
		t.Error("a project larger than the budget is not marked as truncated")
	}
	if p.bytes > maxProjectBytes+2*maxLoadBatch*(1<<20) {
		t.Errorf("the project holds %d bytes", p.bytes)
	}
}

type onceSource struct {
	memorySource
	seen map[string]*model.Index
}

func (s onceSource) LoadAll(paths []string) []*model.Index {
	out := make([]*model.Index, len(paths))
	for i, path := range paths {
		if _, known := s.seen[path]; !known {
			s.seen[path] = s.memorySource.LoadAll([]string{path})[0]
		}
		out[i] = s.seen[path]
	}
	return out
}

func TestALockedInputDescribesItsRevisionAndDate(t *testing.T) {
	t.Parallel()

	lock, ok := parseFlakeLock([]byte(`{"nodes": {"a": {"locked": {"narHash": "sha256-VVTPf+Hyd5ebpjBMHmrLMSBIeW6ls48Bqtosj7CNKLA=", "rev": "0123456789abcdef0123", "lastModified": 86400}}, "root": {"inputs": {"a": "a"}}}, "root": "root", "version": 7}`))
	if !ok {
		t.Fatal("the lock does not parse")
	}
	want := "Locked at `0123456789ab` from 1970-01-02 in `" + lockedStore + "`"
	if got, found := lock.describe("a"); !found || got != want {
		t.Errorf("describe = %q, %v, want %q", got, found, want)
	}
	if got, found := lock.describe("absent"); found {
		t.Errorf("an input that is not locked = %q", got)
	}
}
