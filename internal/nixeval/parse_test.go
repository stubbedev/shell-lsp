package nixeval

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const capturedMetadata = `{
  "path": "/nix/store/abc-source",
  "locked": {"narHash": "sha256-AAAA=", "dir": "sub", "type": "git"},
  "locks": {
    "root": "root",
    "nodes": {
      "root": {"inputs": {"nixpkgs": "nixpkgs_2", "parts": "flake-parts", "pinned": ["parts", "nixpkgs-lib"]}},
      "nixpkgs_2": {"locked": {"owner": "NixOS", "repo": "nixpkgs", "type": "github"}},
      "flake-parts": {"locked": {"owner": "hercules-ci", "repo": "flake-parts", "type": "github"}}
    },
    "version": 7
  }
}`

func TestParseMetadataBuildsALockedReference(t *testing.T) {
	t.Parallel()

	got, err := parseMetadata([]byte(capturedMetadata))
	if err != nil {
		t.Fatal(err)
	}
	if want := "path:/nix/store/abc-source?dir=sub&narHash=sha256-AAAA%3D"; got.ref != want {
		t.Errorf("ref = %s, want %s", got.ref, want)
	}
	if input, err := got.nixpkgsInput(); err != nil || input != "nixpkgs" {
		t.Errorf("nixpkgsInput() = %q, %v, want nixpkgs", input, err)
	}
}

func TestParseMetadataRejectsIncompleteOutput(t *testing.T) {
	t.Parallel()

	for _, in := range []string{
		`not json`,
		`{"locked": {"narHash": "sha256-A="}, "locks": {"root": "root", "nodes": {"root": {}}}}`,
		`{"path": "/nix/store/a", "locks": {"root": "root", "nodes": {"root": {}}}}`,
		`{"path": "/nix/store/a", "locked": {"narHash": "sha256-A="}, "locks": {"root": "root", "nodes": {}}}`,
	} {
		if _, err := parseMetadata([]byte(in)); err == nil {
			t.Errorf("parseMetadata(%s) = nil error", in)
		}
	}
}

func TestNixpkgsInputChoosesOnlyWhatTheFlakeLocks(t *testing.T) {
	t.Parallel()

	metadataWith := func(nodes string) []byte {
		return []byte(`{"path": "/nix/store/a", "locked": {"narHash": "sha256-A="}, "locks": {"root": "root", "nodes": {` + nodes + `}}}`)
	}
	tests := []struct {
		name    string
		nodes   string
		want    string
		wantErr string
	}{
		{
			name:  "the name wins over other nixpkgs inputs",
			nodes: `"root": {"inputs": {"nixpkgs": "a", "unstable": "b"}}, "a": {"locked": {"repo": "nixpkgs"}}, "b": {"locked": {"repo": "nixpkgs"}}`,
			want:  "nixpkgs",
		},
		{
			name:  "a followed input keeps its name",
			nodes: `"root": {"inputs": {"nixpkgs": ["other", "nixpkgs"]}}`,
			want:  "nixpkgs",
		},
		{
			name:  "a single renamed nixpkgs",
			nodes: `"root": {"inputs": {"unstable": "b", "tools": "c"}}, "b": {"locked": {"repo": "nixpkgs"}}, "c": {"locked": {"repo": "tools"}}`,
			want:  "unstable",
		},
		{
			name:  "a registry reference",
			nodes: `"root": {"inputs": {"pkgs": "b"}}, "b": {"original": {"id": "nixpkgs", "type": "indirect"}}`,
			want:  "pkgs",
		},
		{
			name:    "two renamed inputs are ambiguous",
			nodes:   `"root": {"inputs": {"stable": "b", "unstable": "c"}}, "b": {"locked": {"repo": "nixpkgs"}}, "c": {"locked": {"repo": "nixpkgs"}}`,
			wantErr: "stable, unstable",
		},
		{
			name:    "no nixpkgs at all",
			nodes:   `"root": {"inputs": {"tools": "c"}}, "c": {"locked": {"repo": "tools"}}`,
			wantErr: "no root input",
		},
	}
	for _, tt := range tests {
		locked, err := parseMetadata(metadataWith(tt.nodes))
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		got, err := locked.nixpkgsInput()
		if tt.wantErr != "" {
			if !errors.Is(err, ErrNoNixpkgsInput) || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("%s: error = %v, want ErrNoNixpkgsInput naming %q", tt.name, err, tt.wantErr)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("%s: nixpkgsInput() = %q, %v, want %q", tt.name, got, err, tt.want)
		}
	}
}

const capturedShow = `{
  "apps": {"x86_64-linux": {"a": {"type": "app"}}},
  "custom": {"type": "unknown"},
  "legacyPackages": {"x86_64-linux": {}},
  "nixosConfigurations": {"n": {"type": "nixos-configuration"}},
  "overlays": {"o": {"type": "nixpkgs-overlay"}},
  "packages": {
    "aarch64-darwin": {"default": {}},
    "x86_64-linux": {
      "default": {"description": "A package", "name": "h-1", "type": "derivation"},
      "type": {"description": "", "name": "t", "type": "derivation"}
    }
  },
  "templates": {"t": {"description": "tpl", "type": "template"}}
}`

func TestParseOutputsFlattensInSortedOrder(t *testing.T) {
	t.Parallel()

	got, err := parseOutputs([]byte(capturedShow))
	if err != nil {
		t.Fatal(err)
	}
	want := []Output{
		{Path: []string{"apps", "x86_64-linux", "a"}, Kind: OutputApp},
		{Path: []string{"custom"}, Kind: OutputUnknown},
		{Path: []string{"legacyPackages", "x86_64-linux"}, Kind: OutputNotEvaluated},
		{Path: []string{"nixosConfigurations", "n"}, Kind: OutputNixOSConfig},
		{Path: []string{"overlays", "o"}, Kind: OutputOverlay},
		{Path: []string{"packages", "aarch64-darwin", "default"}, Kind: OutputNotEvaluated},
		{Path: []string{"packages", "x86_64-linux", "default"}, Kind: OutputDerivation, Name: "h-1", Description: "A package"},
		{Path: []string{"packages", "x86_64-linux", "type"}, Kind: OutputDerivation, Name: "t"},
		{Path: []string{"templates", "t"}, Kind: OutputTemplate, Description: "tpl"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("parseOutputs() mismatch (-want +got):\n%s", diff)
	}
}

func TestParseOutputsKeepsAKindItDoesNotKnow(t *testing.T) {
	t.Parallel()

	got, err := parseOutputs([]byte(`{"future": {"type": "new-kind"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != "new-kind" {
		t.Errorf("parseOutputs() = %+v, want the new kind kept", got)
	}
}

func TestParseOutputsRejectsMalformedOutput(t *testing.T) {
	t.Parallel()

	for _, in := range []string{`[]`, `{"a": 5}`, `{"a": {"type": "app", "name": 5}}`} {
		if _, err := parseOutputs([]byte(in)); err == nil {
			t.Errorf("parseOutputs(%s) = nil error", in)
		}
	}
}

func TestWithPrefixSelectsTheSortedRange(t *testing.T) {
	t.Parallel()

	names, err := parseNames([]byte(`["rich", "requests", "rel", "a", "requests", "s"]`))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		prefix string
		want   []string
	}{
		{"", []string{"a", "rel", "requests", "rich", "s"}},
		{"re", []string{"rel", "requests"}},
		{"requests", []string{"requests"}},
		{"z", []string{}},
	}
	for _, tt := range tests {
		if got := withPrefix(names, tt.prefix); !slices.Equal(got, tt.want) {
			t.Errorf("withPrefix(%q) = %q, want %q", tt.prefix, got, tt.want)
		}
	}
}

func TestParsePackageDistinguishesNoPackage(t *testing.T) {
	t.Parallel()

	missing, err := parsePackage([]byte(`null`))
	if err != nil || missing.found {
		t.Errorf("parsePackage(null) = %+v, %v, want not found", missing, err)
	}
	got, err := parsePackage([]byte(`{"name": "h-1", "version": "1", "description": "d", "position": "/pkgs/a:b/h.nix:58"}`))
	if err != nil {
		t.Fatal(err)
	}
	want := Package{Name: "h-1", Version: "1", Description: "d", Declaration: Declaration{File: "/pkgs/a:b/h.nix", Line: 58}}
	if !got.found || got.pkg != want {
		t.Errorf("parsePackage() = %+v, want %+v", got, want)
	}
}

func TestParsePositionWithoutALine(t *testing.T) {
	t.Parallel()

	for _, in := range []string{"", "/pkgs/h.nix", "/pkgs/h.nix:x"} {
		if got := parsePosition(in); got != (Declaration{File: in}) {
			t.Errorf("parsePosition(%q) = %+v", in, got)
		}
	}
}

func TestParseSourcesOrdersByKindThenName(t *testing.T) {
	t.Parallel()

	got, err := parseSources([]byte(`{"flake-parts": true, "home-manager": ["b", "a"], "nixos": ["z", "y"]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []OptionSource{
		{Kind: SourceNixOS, Name: "y"},
		{Kind: SourceNixOS, Name: "z"},
		{Kind: SourceHomeManager, Name: "a"},
		{Kind: SourceHomeManager, Name: "b"},
		{Kind: SourceFlakeParts},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("parseSources() mismatch (-want +got):\n%s", diff)
	}
}

const capturedOptions = `[
  {"loc": ["services", "web", "vhosts", "<name>", "root"], "type": "string", "description": "Root", "default": null, "example": null, "declarations": []},
  {"loc": ["services", "web", "vhosts"], "type": "attribute set of submodule", "description": "Hosts", "default": "{ }", "example": null, "declarations": [{"file": "/m.nix", "line": 3, "column": 5}]},
  {"loc": ["services", "web", "enable"], "type": "boolean", "description": "Enable", "default": "false", "example": "true", "declarations": [{"file": "/m.nix"}]},
  {"loc": ["services", "web", "enable"], "type": "duplicate", "description": null, "default": null, "example": null, "declarations": []},
  {"loc": ["items", "*", "name"], "type": "string", "description": null, "default": null, "example": null, "declarations": []}
]`

func childNames(n *OptionNode) []string {
	names := []string{}
	for child := range n.Children() {
		names = append(names, child.Name())
	}
	return names
}

func TestParseOptionsBuildsASortedTree(t *testing.T) {
	t.Parallel()

	tree, err := parseOptions([]byte(capturedOptions))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := childNames(tree.Root()), []string{"items", "services"}; !slices.Equal(got, want) {
		t.Errorf("root children = %q, want %q", got, want)
	}
	web, ok := tree.Lookup([]string{"services", "web"})
	if !ok {
		t.Fatal("services.web is missing")
	}
	if _, isOption := web.Option(); isOption {
		t.Error("services.web holds an option, want only children")
	}
	if got, want := childNames(web), []string{"enable", "vhosts"}; !slices.Equal(got, want) {
		t.Errorf("services.web children = %q, want %q", got, want)
	}
	enable, ok := tree.Lookup([]string{"services", "web", "enable"})
	if !ok {
		t.Fatal("services.web.enable is missing")
	}
	got, isOption := enable.Option()
	want := Option{
		Path:         []string{"services", "web", "enable"},
		Type:         "boolean",
		Description:  "Enable",
		Default:      "false",
		Example:      "true",
		Declarations: []Declaration{{File: "/m.nix"}},
	}
	if diff := cmp.Diff(want, got); !isOption || diff != "" {
		t.Errorf("enable.Option() is option %v, mismatch (-want +got):\n%s", isOption, diff)
	}
}

func TestLookupFollowsPlaceholdersOnlyWithoutAnExactChild(t *testing.T) {
	t.Parallel()

	tree, err := parseOptions([]byte(capturedOptions))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range [][]string{
		{"services", "web", "vhosts", "example.com", "root"},
		{"services", "web", "vhosts", "<name>", "root"},
		{"items", "first", "name"},
	} {
		node, ok := tree.Lookup(path)
		if !ok {
			t.Errorf("Lookup(%q) missing", path)
			continue
		}
		if _, isOption := node.Option(); !isOption {
			t.Errorf("Lookup(%q) holds no option", path)
		}
	}
	if _, ok := tree.Lookup([]string{"services", "absent"}); ok {
		t.Error("Lookup(services.absent) found a node")
	}
	vhosts, _ := tree.Lookup([]string{"services", "web", "vhosts"})
	for child := range vhosts.Children() {
		if !child.IsPlaceholder() {
			t.Errorf("child %q is not a placeholder", child.Name())
		}
	}
}

func TestOptionReturnsACopy(t *testing.T) {
	t.Parallel()

	tree, err := parseOptions([]byte(capturedOptions))
	if err != nil {
		t.Fatal(err)
	}
	node, _ := tree.Lookup([]string{"services", "web", "enable"})
	first, _ := node.Option()
	first.Path[0] = "changed"
	first.Declarations[0].File = "changed"
	second, _ := node.Option()
	if second.Path[0] != "services" || second.Declarations[0].File != "/m.nix" {
		t.Errorf("Option() shares state with the cached tree: %+v", second)
	}
}

func TestParseOptionsIsIndependentOfInputOrder(t *testing.T) {
	t.Parallel()

	reversed := `[
  {"loc": ["b"], "type": "second"},
  {"loc": ["a", "y"], "type": "y"},
  {"loc": ["a", "x"], "type": "x"},
  {"loc": ["a"], "type": "a"}
]`
	tree, err := parseOptions([]byte(reversed))
	if err != nil {
		t.Fatal(err)
	}
	node, ok := tree.Lookup([]string{"a"})
	if !ok {
		t.Fatal("a is missing")
	}
	if got, want := childNames(node), []string{"x", "y"}; !slices.Equal(got, want) {
		t.Errorf("a children = %q, want %q", got, want)
	}
	if option, isOption := node.Option(); !isOption || option.Type != "a" {
		t.Errorf("a.Option() = %+v, %v", option, isOption)
	}
}

func TestParseOptionsRejectsMalformedOutput(t *testing.T) {
	t.Parallel()

	for _, in := range []string{`{}`, `[null]`, `[{"loc": []}]`, `[{"type": "x"}]`, `[{"loc": [1]}]`} {
		if _, err := parseOptions([]byte(in)); err == nil {
			t.Errorf("parseOptions(%s) = nil error", in)
		}
	}
}

func TestParseOptionsHandlesAVeryDeepLocation(t *testing.T) {
	t.Parallel()

	in := `[{"loc": ["a"` + strings.Repeat(`,"a"`, 2_000_000) + `]}]`
	tree, err := parseOptions([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tree.Lookup([]string{"a", "a", "a"}); !ok {
		t.Error("the deep location is missing")
	}
}

func TestParseSystem(t *testing.T) {
	t.Parallel()

	if got, err := parseSystem([]byte("x86_64-linux\n")); err != nil || got != "x86_64-linux" {
		t.Errorf("parseSystem() = %q, %v", got, err)
	}
	if _, err := parseSystem([]byte("\n")); err == nil {
		t.Error("parseSystem(empty) = nil error")
	}
}
