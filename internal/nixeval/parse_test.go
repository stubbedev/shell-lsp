package nixeval

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const (
	capturedMetadata = `{
  "locked": {"lastModified": 0, "dir": "sub", "type": "git", "url": "file:///repo"},
  "locks": {"root": "root", "nodes": {"root": {}}, "version": 7}
}`
	capturedPrefetch = `{
  "hash": "sha256-AAAA=",
  "locked": {"lastModified": 0, "type": "git", "url": "file:///repo"},
  "original": {"type": "git", "url": "file:///repo"},
  "storePath": "/nix/store/abc-source"
}`
)

func TestMetadataAndPrefetchBuildALockedReference(t *testing.T) {
	t.Parallel()

	dir, err := parseMetadata([]byte(capturedMetadata))
	if err != nil {
		t.Fatal(err)
	}
	got, err := parsePrefetch([]byte(capturedPrefetch), dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := "path:/nix/store/abc-source?dir=sub&narHash=sha256-AAAA%3D"; got.ref != want {
		t.Errorf("ref = %s, want %s", got.ref, want)
	}
}

func TestParseMetadataRejectsIncompleteOutput(t *testing.T) {
	t.Parallel()

	for _, in := range []string{
		`not json`,
		`{"path": "/nix/store/a"}`,
	} {
		if _, err := parseMetadata([]byte(in)); err == nil {
			t.Errorf("parseMetadata(%s) = nil error", in)
		}
	}
}

func TestParsePrefetchRejectsIncompleteOutput(t *testing.T) {
	t.Parallel()

	for _, in := range []string{
		`not json`,
		`{"hash": "sha256-A="}`,
		`{"storePath": "/nix/store/a"}`,
	} {
		if _, err := parsePrefetch([]byte(in), ""); err == nil {
			t.Errorf("parsePrefetch(%s) = nil error", in)
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
	if err != nil || missing != nil {
		t.Errorf("parsePackage(null) = %+v, %v, want not found", missing, err)
	}
	got, err := parsePackage([]byte(`{"name": "h-1", "version": "1", "description": "d", "position": "/pkgs/a:b/h.nix:58"}`))
	if err != nil {
		t.Fatal(err)
	}
	want := Package{Name: "h-1", Version: "1", Description: "d", Declaration: Declaration{File: "/pkgs/a:b/h.nix", Line: 58}}
	if got == nil || *got != want {
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

func parseOptions(list []byte) (*OptionTree, error) {
	encoded, err := encodeOptions([]byte(`{"options": ` + string(list) + `, "truncated": []}`))
	if err != nil {
		return nil, err
	}
	return decodeOptions(encoded)
}

func childNames(n OptionNode) []string {
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

func TestParseOutputsReadsTheDeterminateInventory(t *testing.T) {
	t.Parallel()

	got, err := parseOutputs([]byte(`{
  "version": 2,
  "inventory": {
    "apps": {"doc": "d", "output": {"children": {"x86_64-linux": {"children": {"default": {"what": "app", "shortDescription": "an app"}}}}}},
    "lib": {"unknown": true},
    "legacyPackages": {"doc": "d", "output": {"children": {"x86_64-linux": {"isLegacy": true}}}},
    "nixosConfigurations": {"doc": "d", "output": {"children": {"box": {"what": "NixOS configuration", "derivation": {"name": "nixos-system"}, "forSystems": ["x86_64-linux"]}}}},
    "nixosModules": {"doc": "d", "output": {"children": {"default": {"what": "NixOS module"}}}},
    "overlays": {"doc": "d", "output": {"children": {"default": {"what": "Nixpkgs overlay"}}}},
    "packages": {"doc": "d", "output": {"children": {
      "aarch64-linux": {"filtered": true},
      "x86_64-linux": {"children": {"default": {"derivation": {"name": "app-1.0"}, "forSystems": ["x86_64-linux"], "shortDescription": "An app", "what": "package"}}}
    }}},
    "schemaWithoutKnownName": {"doc": "d", "output": {"what": "something new"}},
    "templates": {"doc": "d", "output": {"children": {"default": {"what": "template", "shortDescription": "A template"}}}}
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []Output{
		{Path: []string{"apps", "x86_64-linux", "default"}, Kind: OutputApp, Description: "an app"},
		{Path: []string{"legacyPackages", "x86_64-linux"}, Kind: OutputNotEvaluated},
		{Path: []string{"lib"}, Kind: OutputUnknown},
		{Path: []string{"nixosConfigurations", "box"}, Kind: OutputNixOSConfig, Name: "nixos-system"},
		{Path: []string{"nixosModules", "default"}, Kind: OutputNixOSModule},
		{Path: []string{"overlays", "default"}, Kind: OutputOverlay},
		{Path: []string{"packages", "aarch64-linux"}, Kind: OutputNotEvaluated},
		{Path: []string{"packages", "x86_64-linux", "default"}, Kind: OutputDerivation, Name: "app-1.0", Description: "An app"},
		{Path: []string{"schemaWithoutKnownName"}, Kind: OutputUnknown},
		{Path: []string{"templates", "default"}, Kind: OutputTemplate, Description: "A template"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Error(diff)
	}
}

func TestBothFormatsOfOneFlakeAgreeOnWhatTheyBothReport(t *testing.T) {
	t.Parallel()

	legacy, err := parseOutputs([]byte(`{
  "packages": {"x86_64-linux": {"default": {"type": "derivation", "name": "app-1.0", "description": "An app"}}},
  "lib": {"type": "unknown"}
}`))
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := parseOutputs([]byte(`{"version": 2, "inventory": {
  "packages": {"output": {"children": {"x86_64-linux": {"children": {"default": {"derivation": {"name": "app-1.0"}, "shortDescription": "An app", "what": "package"}}}}}},
  "lib": {"unknown": true}
}}`))
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(legacy, inventory); diff != "" {
		t.Error(diff)
	}
}

func TestAFlakeOutputNamedInventoryStaysAnOutput(t *testing.T) {
	t.Parallel()

	got, err := parseOutputs([]byte(`{"inventory": {"type": "unknown"}, "version": {"type": "unknown"}}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []Output{{Path: []string{"inventory"}, Kind: OutputUnknown}, {Path: []string{"version"}, Kind: OutputUnknown}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Error(diff)
	}
}

func TestParseOutputsRejectsAnInventoryItCannotRead(t *testing.T) {
	t.Parallel()

	for _, in := range []string{
		`{"version": 3, "inventory": {}}`,
		`{"version": 2, "inventory": 5}`,
		`{"version": 2, "inventory": {"packages": {}}}`,
		`{"version": 2, "inventory": {"packages": {"output": 5}}}`,
	} {
		if got, err := parseOutputs([]byte(in)); err == nil {
			t.Errorf("parseOutputs(%s) = %+v, want an error", in, got)
		}
	}
}
