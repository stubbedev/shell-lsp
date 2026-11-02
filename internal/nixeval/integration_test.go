//go:build unix

package nixeval

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const fixtureNixpkgs = `{
  outputs = { self }: let
    packagesFor = system: let
      package = name: derivation { inherit name system; builder = "/bin/sh"; } // {
        version = "1.0";
        meta = { description = "The ${name} package"; position = "/pkgs/${name}.nix:7"; };
      };
    in {
      hello = package "hello";
      broken = throw "broken on purpose";
      notPackage = 5;
      python3Packages = { requests = package "requests"; rich = package "rich"; };
      "evil\${throw \"INJECTED\"}\"; x = \"" = package "evil";
    };
  in {
    legacyPackages = builtins.listToAttrs (map (system: { name = system; value = packagesFor system; })
      [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ]);
  };
}
`

const fixtureOptionHelper = `leaf = loc: attrs: {
      _type = "option";
      inherit loc;
      type = { description = "string"; getSubOptions = _: { }; };
      description = "Option ${builtins.concatStringsSep "." loc}";
      declarations = [ "/modules/fixture.nix" ];
      declarationPositions = [ { file = "/modules/fixture.nix"; line = 3; column = 5; } ];
    } // attrs;`

const fixtureSingleFile = `{
  inputs.nixpkgs.url = "path:%s";
  outputs = { self, nixpkgs }: let
    ` + fixtureOptionHelper + `
    drv = system: derivation { name = "app"; inherit system; builder = "/bin/sh"; };
    leafAt = base: name: attrs: leaf (base ++ [ name ]) attrs;
    web = leafAt [ "services" "web" ];
  in {
    packages = builtins.listToAttrs (map (system: { name = system; value.default = drv system; })
      [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ]);
    nixosConfigurations.box.options = {
      services.web = {
        enable = web "enable" { default = false; example = true; };
        package = web "package" { default = nixpkgs.legacyPackages.x86_64-linux.hello; };
        rendered = web "rendered" { default = throw "never forced"; defaultText = { _type = "literalExpression"; text = "pkgs.hello"; }; };
        failing = web "failing" { default = throw "unevaluable"; };
        nested = web "nested" { default = { a = [ 1 "two" ]; "b c" = null; }; };
        hidden = web "hidden" { internal = true; };
        invisible = web "invisible" { visible = false; type = { description = "x"; getSubOptions = prefix: { under = leaf (prefix ++ [ "under" ]) { }; }; }; };
        vhosts = web "vhosts" {
          type = {
            description = "attribute set of submodule";
            getSubOptions = prefix: { root = leaf (prefix ++ [ "<name>" "root" ]) { }; };
          };
        };
      };
    };
    homeConfigurations.me.options.programs.git.enable = leaf [ "programs" "git" "enable" ] { default = false; };
  };
}
`

const fixtureFakeParts = `{
  outputs = { self }: {
    lib = rec {
      evalFlakeModule = args: module: {
        options = module.options;
        config.flake = module.flake;
      };
      mkFlake = args: module: (evalFlakeModule args module).config.flake;
    };
  };
}
`

const fixtureUsesParts = `{
  inputs.parts.url = "path:%s";
  outputs = inputs: let
    ` + fixtureOptionHelper + `
  in inputs.parts.lib.mkFlake { inherit inputs; } {
    options.perSystem = leaf [ "perSystem" ] {
      type = {
        description = "module";
        getSubOptions = prefix: { packages = leaf (prefix ++ [ "packages" ]) { }; };
      };
    };
    flake.lib.answer = 42;
  };
}
`

func realClient(t *testing.T) *Client {
	t.Helper()
	h := newHarness(t, execRunner(nixProgram))
	h.settings.identify = nixIdentity(nixProgram)
	return h.client(t)
}

func requireNix(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath(nixProgram); err != nil {
		t.Skip("nix is not installed; the evaluator features need it")
	}
}

func writeFixture(t *testing.T, dir, nix string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "flake.nix"), nix)
	return dir
}

func offlineLock(t *testing.T, dir string) ([]byte, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), nixProgram, "flake", "lock", "--offline", "--extra-experimental-features", experimental)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

func lockFixture(t *testing.T, dir string) {
	t.Helper()
	if out, err := offlineLock(t, dir); err != nil {
		t.Fatalf("locking the fixture in %s: %v\n%s", dir, err, out)
	}
}

func lockedFixture(t *testing.T, template, input string) string {
	t.Helper()
	root := t.TempDir()
	dep := writeFixture(t, filepath.Join(root, "dep"), input)
	dir := writeFixture(t, filepath.Join(root, "flake"), fmt.Sprintf(template, dep))
	lockFixture(t, dir)
	return dir
}

func TestRealNixRoundTripsHostileStrings(t *testing.T) {
	t.Parallel()
	requireNix(t)

	for _, hostile := range []string{
		`${throw "INJECTED"}`,
		`\${throw "INJECTED"}`,
		`$${throw "INJECTED"}`,
		`"; throw "INJECTED"; "`,
		`\"; throw "INJECTED"; \"`,
		`''${throw "INJECTED"}''`,
		"line\nbreak\t$",
		`trailing\`,
		`%s #?&=`,
	} {
		out, err := runProgram(t.Context(), nixProgram, t.TempDir(), evalArgs(nixString(hostile)))
		if err != nil {
			t.Errorf("evaluating the literal of %q: %v", hostile, err)
			continue
		}
		var got string
		if err := json.Unmarshal(out, &got); err != nil {
			t.Errorf("decoding %s: %v", out, err)
			continue
		}
		if got != hostile {
			t.Errorf("the literal of %q evaluated to %q", hostile, got)
		}
	}
}

func TestRealHostileAttributeNamesStayData(t *testing.T) {
	t.Parallel()
	requireNix(t)

	dir := lockedFixture(t, fixtureSingleFile, fixtureNixpkgs)
	c := realClient(t)
	const injected = `${throw "INJECTED"}`
	const closes = `"; x = throw "INJECTED"; y = "`
	for _, hostile := range []string{injected, closes, `\` + injected, "''" + injected} {
		names, err := freshOf(c.NixpkgsAttrs(t.Context(), dir, []string{hostile}, ""))
		if err != nil {
			t.Errorf("NixpkgsAttrs(%q) error = %v", hostile, err)
		}
		if len(names) != 0 {
			t.Errorf("NixpkgsAttrs(%q) = %q, want none", hostile, names)
		}
		if pkg, err := freshOf(c.NixpkgsPackage(t.Context(), dir, []string{hostile})); err != nil || pkg != nil {
			t.Errorf("NixpkgsPackage(%q) = %+v, %v, want nothing", hostile, pkg, err)
		}
		_, err = freshOf(c.Options(t.Context(), dir, OptionSource{Kind: SourceNixOS, Name: hostile}))
		if err == nil || strings.Contains(err.Error(), "error: INJECTED") {
			t.Errorf("Options(%q) error = %v, want a missing attribute and no injected throw", hostile, err)
		}
	}
	evil := `evil${throw "INJECTED"}"; x = "`
	names, err := freshOf(c.NixpkgsAttrs(t.Context(), dir, nil, "evil"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(names, []string{evil}) {
		t.Fatalf("NixpkgsAttrs(evil) = %q, want the hostile name as data", names)
	}
	pkg, err := freshOf(c.NixpkgsPackage(t.Context(), dir, []string{evil}))
	if err != nil || pkg == nil || pkg.Name != "evil" {
		t.Errorf("NixpkgsPackage(evil name) = %+v, %v", pkg, err)
	}
}

func TestRealHostileDirectoryNameStaysData(t *testing.T) {
	t.Parallel()
	requireNix(t)

	dir := writeFixture(t, filepath.Join(t.TempDir(), `x#y?dir=z%20"${throw "INJECTED"}`), `{ outputs = { self }: { lib.answer = 42; }; }`+"\n")
	got, err := freshOf(realClient(t).Outputs(t.Context(), dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !slices.Equal(got[0].Path, []string{"lib"}) {
		t.Errorf("Outputs() = %+v", got)
	}
}

func TestRealSingleFileFlake(t *testing.T) {
	t.Parallel()
	requireNix(t)

	dir := lockedFixture(t, fixtureSingleFile, fixtureNixpkgs)
	c := realClient(t)

	outputs, err := freshOf(c.Outputs(t.Context(), dir))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(outputs, func(o Output) bool {
		return o.Kind == OutputDerivation && o.Name == "app" && o.Path[0] == "packages"
	}) {
		t.Errorf("Outputs() = %+v, want the package of this system", outputs)
	}
	if !slices.ContainsFunc(outputs, func(o Output) bool {
		return o.Kind == OutputNotEvaluated && o.Path[0] == "packages"
	}) {
		t.Errorf("Outputs() = %+v, want other systems marked as not evaluated", outputs)
	}

	sources, err := freshOf(c.OptionSources(t.Context(), dir))
	if err != nil {
		t.Fatal(err)
	}
	wantSources := []OptionSource{{Kind: SourceNixOS, Name: "box"}, {Kind: SourceHomeManager, Name: "me"}}
	if !slices.Equal(sources, wantSources) {
		t.Errorf("OptionSources() = %+v, want %+v", sources, wantSources)
	}

	names, err := freshOf(c.NixpkgsAttrs(t.Context(), dir, []string{"python3Packages"}, "re"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(names, []string{"requests"}) {
		t.Errorf("NixpkgsAttrs(python3Packages, re) = %q", names)
	}
	for _, path := range [][]string{{"absent"}, {"notPackage"}, {"notPackage", "deeper"}} {
		if names, err := freshOf(c.NixpkgsAttrs(t.Context(), dir, path, "")); err != nil || len(names) != 0 {
			t.Errorf("NixpkgsAttrs(%q) = %q, %v, want none", path, names, err)
		}
	}
	if _, err := freshOf(c.NixpkgsAttrs(t.Context(), dir, []string{"broken"}, "")); err == nil || !strings.Contains(err.Error(), "broken on purpose") {
		t.Errorf("NixpkgsAttrs(broken) error = %v, want the throw reported", err)
	}

	pkg, err := freshOf(c.NixpkgsPackage(t.Context(), dir, []string{"hello"}))
	if err != nil || pkg == nil {
		t.Fatalf("NixpkgsPackage(hello) = %+v, %v", pkg, err)
	}
	wantPkg := Package{Name: "hello", Version: "1.0", Description: "The hello package", Declaration: Declaration{File: "/pkgs/hello.nix", Line: 7}}
	if *pkg != wantPkg {
		t.Errorf("NixpkgsPackage(hello) = %+v, want %+v", pkg, wantPkg)
	}
	for _, path := range [][]string{{"broken"}, {"absent"}, {"python3Packages"}, {"notPackage"}} {
		if pkg, err := freshOf(c.NixpkgsPackage(t.Context(), dir, path)); err != nil || pkg != nil {
			t.Errorf("NixpkgsPackage(%q) = %+v, %v, want nothing", path, pkg, err)
		}
	}

	nixos, err := freshOf(c.Options(t.Context(), dir, OptionSource{Kind: SourceNixOS, Name: "box"}))
	if err != nil {
		t.Fatal(err)
	}
	checkFixtureOptions(t, nixos)

	home, err := freshOf(c.Options(t.Context(), dir, OptionSource{Kind: SourceHomeManager, Name: "me"}))
	if err != nil {
		t.Fatal(err)
	}
	if option := mustOption(t, home, "programs", "git", "enable"); option.Default != "false" || option.Type != "string" {
		t.Errorf("programs.git.enable = %+v", option)
	}

	if _, err := os.Stat(filepath.Join(dir, "flake.lock")); err != nil {
		t.Errorf("flake.lock of the fixture: %v", err)
	}
}

func mustOption(t *testing.T, tree *OptionTree, path ...string) Option {
	t.Helper()
	node, ok := tree.Lookup(path)
	if !ok {
		t.Fatalf("option %q is missing", path)
	}
	option, ok := node.Option()
	if !ok {
		t.Fatalf("%q holds no option", path)
	}
	return option
}

func checkFixtureOptions(t *testing.T, tree *OptionTree) {
	t.Helper()
	enable := mustOption(t, tree, "services", "web", "enable")
	if enable.Type != "string" || enable.Default != "false" || enable.Example != "true" || enable.Description != "Option services.web.enable" {
		t.Errorf("enable = %+v", enable)
	}
	if want := []Declaration{{File: "/modules/fixture.nix", Line: 3, Column: 5}}; !slices.Equal(enable.Declarations, want) {
		t.Errorf("enable declarations = %+v, want %+v", enable.Declarations, want)
	}
	for _, tt := range []struct{ name, want string }{
		{"package", "«derivation»"},
		{"rendered", "pkgs.hello"},
		{"failing", "«failed to evaluate»"},
		{"nested", `{ a = [ 1 "two" ]; "b c" = null; }`},
	} {
		if got := mustOption(t, tree, "services", "web", tt.name).Default; got != tt.want {
			t.Errorf("%s default = %q, want %q", tt.name, got, tt.want)
		}
	}
	for _, hidden := range []string{"hidden", "invisible"} {
		if _, ok := tree.Lookup([]string{"services", "web", hidden}); ok {
			t.Errorf("%s is listed, want it hidden", hidden)
		}
	}
	root := mustOption(t, tree, "services", "web", "vhosts", "example.org", "root")
	if !slices.Equal(root.Path, []string{"services", "web", "vhosts", "<name>", "root"}) {
		t.Errorf("sub-option path = %q", root.Path)
	}
}

func TestRealFlakePartsOptionsAreCapturedFromMkFlake(t *testing.T) {
	t.Parallel()
	requireNix(t)

	dir := lockedFixture(t, fixtureUsesParts, fixtureFakeParts)
	c := realClient(t)
	sources, err := freshOf(c.OptionSources(t.Context(), dir))
	if err != nil {
		t.Fatal(err)
	}
	if want := []OptionSource{{Kind: SourceFlakeParts}}; !slices.Equal(sources, want) {
		t.Fatalf("OptionSources() = %+v, want %+v", sources, want)
	}
	tree, err := freshOf(c.Options(t.Context(), dir, sources[0]))
	if err != nil {
		t.Fatal(err)
	}
	mustOption(t, tree, "perSystem", "packages")
	outputs, err := freshOf(c.Outputs(t.Context(), dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) != 1 || outputs[0].Path[0] != "lib" {
		t.Errorf("Outputs() = %+v, want the real outputs without the captured options", outputs)
	}
}

func TestRealSubdirectoryOfADirtyGitTree(t *testing.T) {
	t.Parallel()
	requireNix(t)
	requireGit(t)

	root := t.TempDir()
	dir := writeFixture(t, filepath.Join(root, "sub"), fmt.Sprintf(fixtureUsesParts, "./parts"))
	writeFixture(t, filepath.Join(dir, "parts"), fixtureFakeParts)
	git := gitIn(t, root)
	git("init", "-q")
	git("add", ".")
	lockFixture(t, dir)
	git("add", ".")
	writeFile(t, filepath.Join(dir, "untracked.txt"), "dirty\n")

	sources, err := freshOf(realClient(t).OptionSources(t.Context(), dir))
	if err != nil {
		t.Fatal(err)
	}
	if want := []OptionSource{{Kind: SourceFlakeParts}}; !slices.Equal(sources, want) {
		t.Errorf("OptionSources() = %+v, want %+v", sources, want)
	}
}

func TestRealFlakeWithoutNixpkgsIsAPreciseError(t *testing.T) {
	t.Parallel()
	requireNix(t)

	dir := writeFixture(t, t.TempDir(), `{ outputs = { self }: { }; }`+"\n")
	_, err := freshOf(realClient(t).NixpkgsAttrs(t.Context(), dir, nil, ""))
	if !errors.Is(err, ErrNoNixpkgsInput) || !strings.Contains(err.Error(), dir) {
		t.Errorf("error = %v, want ErrNoNixpkgsInput naming %s", err, dir)
	}
}

func TestRealOutdatedLockIsRefusedAndNotRewritten(t *testing.T) {
	t.Parallel()
	requireNix(t)

	root := t.TempDir()
	dep := writeFixture(t, filepath.Join(root, "dep"), fixtureNixpkgs)
	dir := writeFixture(t, filepath.Join(root, "flake"), fmt.Sprintf(`{ inputs.nixpkgs.url = "path:%s"; outputs = { self, nixpkgs }: { }; }`, dep))
	_, err := freshOf(realClient(t).Outputs(t.Context(), dir))
	if err == nil || !strings.Contains(err.Error(), "no-update-lock-file") {
		t.Errorf("error = %v, want a refusal to change the lock file", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "flake.lock")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("flake.lock appeared: %v", statErr)
	}
}

func TestRealFlakeCannotLoosenEvaluationThroughNixConfig(t *testing.T) {
	t.Parallel()
	requireNix(t)

	tests := []struct{ name, outputs, want string }{
		{
			name:    "import from derivation",
			outputs: `import (derivation { name = "ifd"; system = "x86_64-linux"; builder = "/bin/sh"; args = [ "-c" "echo 1 > $out" ]; })`,
			want:    "allow-import-from-derivation",
		},
		{
			name:    "reading outside the store",
			outputs: `builtins.readFile /etc/hostname`,
			want:    "pure evaluation mode",
		},
		{
			name:    "running a program",
			outputs: `builtins.exec [ "true" ]`,
			want:    "'exec'",
		},
	}
	for _, tt := range tests {
		nix := fmt.Sprintf(`{
  nixConfig.allow-import-from-derivation = true;
  nixConfig.pure-eval = false;
  nixConfig.allow-unsafe-native-code-during-evaluation = true;
  outputs = { self }: { nixosConfigurations = %s; };
}
`, tt.outputs)
		dir := writeFixture(t, t.TempDir(), nix)
		_, err := freshOf(realClient(t).OptionSources(t.Context(), dir))
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: error = %v, want a refusal mentioning %q", tt.name, err, tt.want)
		}
	}
}

func TestRealNixpkgsAndFlakeParts(t *testing.T) {
	t.Parallel()
	requireNix(t)
	if testing.Short() {
		t.Skip("evaluating real NixOS options belongs to full test runs")
	}

	nixpkgs, parts := repositoryLockedRevisions(t)
	dir := writeFixture(t, t.TempDir(), fmt.Sprintf(`{
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/%s";
  inputs.flake-parts = { url = "github:hercules-ci/flake-parts/%s"; inputs.nixpkgs-lib.follows = "nixpkgs"; };
  outputs = inputs@{ nixpkgs, flake-parts, ... }: flake-parts.lib.mkFlake { inherit inputs; } {
    systems = [ "x86_64-linux" ];
    flake.nixosConfigurations.box = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [ { boot.isContainer = true; system.stateVersion = "25.05"; } ];
    };
  };
}
`, nixpkgs, parts))
	if out, err := offlineLock(t, dir); err != nil {
		t.Skipf("the inputs this repository locks are not in the local store: %v\n%s", err, out)
	}

	c := realClient(t)
	sources, err := freshOf(c.OptionSources(t.Context(), dir))
	if err != nil {
		t.Fatal(err)
	}
	if want := []OptionSource{{Kind: SourceNixOS, Name: "box"}, {Kind: SourceFlakeParts}}; !slices.Equal(sources, want) {
		t.Fatalf("OptionSources() = %+v, want %+v", sources, want)
	}

	nixos, err := freshOf(c.Options(t.Context(), dir, sources[0]))
	if err != nil {
		t.Fatal(err)
	}
	enable := mustOption(t, nixos, "services", "nginx", "enable")
	if enable.Type != "boolean" || enable.Default != "false" || len(enable.Declarations) == 0 || enable.Declarations[0].Line == 0 ||
		!strings.HasSuffix(enable.Declarations[0].File, "nixos/modules/services/web-servers/nginx/default.nix") {
		t.Errorf("services.nginx.enable = %+v", enable)
	}
	mustOption(t, nixos, "services", "nginx", "virtualHosts", "example.org", "root")

	flakeParts, err := freshOf(c.Options(t.Context(), dir, sources[1]))
	if err != nil {
		t.Fatal(err)
	}
	mustOption(t, flakeParts, "systems")
	mustOption(t, flakeParts, "perSystem", "packages")

	names, err := freshOf(c.NixpkgsAttrs(t.Context(), dir, []string{"python3Packages"}, "requests"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(names, "requests") {
		t.Errorf("NixpkgsAttrs(python3Packages, requests) = %q", names)
	}
	pkg, err := freshOf(c.NixpkgsPackage(t.Context(), dir, []string{"hello"}))
	if err != nil || pkg == nil || pkg.Description == "" || pkg.Declaration.Line == 0 {
		t.Errorf("NixpkgsPackage(hello) = %+v, %v", pkg, err)
	}
}

func repositoryLockedRevisions(t *testing.T) (nixpkgs, flakeParts string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "flake.lock"))
	if err != nil {
		t.Fatal(err)
	}
	var lock struct {
		Nodes map[string]struct {
			Locked struct {
				Rev string `json:"rev"`
			} `json:"locked"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		t.Fatal(err)
	}
	return lock.Nodes["nixpkgs"].Locked.Rev, lock.Nodes["flake-parts"].Locked.Rev
}

const fixtureDiscoversModules = `{
  outputs = { self }: let
    ` + fixtureOptionHelper + `
    files = builtins.attrNames (builtins.readDir ./modules);
    found = map (name: import (./modules + "/${name}") leaf) files;
  in {
    nixosConfigurations.box.options = builtins.foldl' (a: b: a // b) { } found;
  };
}
`

func TestRealAModuleFoundOnlyByReadingADirectoryIsCoveredByTheKey(t *testing.T) {
	t.Parallel()
	requireNix(t)
	requireGit(t)

	dir := writeFixture(t, filepath.Join(t.TempDir(), "flake"), fixtureDiscoversModules)
	modules := filepath.Join(dir, "modules")
	if err := os.Mkdir(modules, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(modules, "first.nix"), `leaf: { first = leaf [ "first" ] { }; }`+"\n")
	git := gitIn(t, dir)
	git("init", "-q")
	git("add", ".")
	lockFixture(t, dir)
	git("add", ".")

	c := realClient(t)
	source := OptionSource{Kind: SourceNixOS, Name: "box"}
	options := func() *OptionTree {
		t.Helper()
		return settled(t, func() (Answer[*OptionTree], error) { return c.Options(t.Context(), dir, source) })
	}
	mustOption(t, options(), "first")

	writeFile(t, filepath.Join(modules, "first.nix"), `leaf: { renamed = leaf [ "renamed" ] { }; }`+"\n")
	tree := options()
	mustOption(t, tree, "renamed")
	if _, ok := tree.Lookup([]string{"first"}); ok {
		t.Error("the edited module still contributes its old option")
	}

	writeFile(t, filepath.Join(modules, "second.nix"), `leaf: { second = leaf [ "second" ] { }; }`+"\n")
	if _, ok := options().Lookup([]string{"second"}); ok {
		t.Error("nix sees an untracked module, so the key must not need it")
	}
	git("add", ".")
	mustOption(t, options(), "second")
}

func TestRealTruncatedBranchesAreReported(t *testing.T) {
	t.Parallel()
	requireNix(t)

	dir := writeFixture(t, t.TempDir(), `{
  outputs = { self }: let
    deep = path: {
      _type = "option";
      loc = path;
      type = { description = "self"; getSubOptions = prefix: { next = deep (prefix ++ [ "next" ]); }; };
    };
  in {
    nixosConfigurations.box.options = { root = deep [ "root" ]; };
  };
}
`)
	tree, err := freshOf(realClient(t).Options(t.Context(), dir, OptionSource{Kind: SourceNixOS, Name: "box"}))
	if err != nil {
		t.Fatal(err)
	}
	truncated := tree.Truncated()
	if len(truncated) != 1 {
		t.Fatalf("Truncated() = %q, want the one endless branch", truncated)
	}
	if !slices.Equal(truncated[0][:2], []string{"root", "next"}) || len(truncated[0]) != 32 {
		t.Errorf("Truncated()[0] = %q, want the path 32 names deep", truncated[0])
	}
	if _, ok := tree.Lookup(truncated[0]); !ok {
		t.Errorf("the deepest listed option %q is missing", truncated[0])
	}
}

func TestRealBaseTreeOfTheLockedNixpkgsEvaluatesUnderTheSafetyFlags(t *testing.T) {
	t.Parallel()
	requireNix(t)
	if testing.Short() {
		t.Skip("evaluating real NixOS options belongs to full test runs")
	}

	nixpkgs, _ := repositoryLockedRevisions(t)
	dir := writeFixture(t, t.TempDir(), fmt.Sprintf(`{
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/%s";
  outputs = { nixpkgs, ... }: {
    nixosConfigurations.box = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [ { boot.isContainer = true; system.stateVersion = "25.05"; } ];
    };
  };
}
`, nixpkgs))
	if out, err := offlineLock(t, dir); err != nil {
		t.Skipf("the inputs this repository locks are not in the local store: %v\n%s", err, out)
	}
	c := realClient(t)
	warmed := c.Warm(dir)
	<-warmed.Done()
	if err := warmed.Err(); err != nil {
		t.Fatal(err)
	}
	other := writeFixture(t, t.TempDir(), fmt.Sprintf(`{
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/%s";
  outputs = { nixpkgs, ... }: {
    nixosConfigurations.box = throw "not evaluated before the shared tree answers";
  };
}
`, nixpkgs))
	if out, err := offlineLock(t, other); err != nil {
		t.Fatalf("locking the second flake: %v\n%s", err, out)
	}
	answer, err := c.Options(t.Context(), other, OptionSource{Kind: SourceNixOS, Name: "box"})
	if err != nil {
		t.Fatal(err)
	}
	tree, stale := answer.Get()
	if stale == nil || stale.Basis != BasisLockedNixpkgs {
		t.Fatalf("stale = %+v, want the locked nixpkgs", stale)
	}
	mustOption(t, tree, "services", "nginx", "enable")
}
