package nix

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

func outputNames(src string) []string {
	var names []string
	tree := syntax.Parse(src)
	for _, r := range outputs(tree, scope.Resolve(tree)) {
		names = append(names, r.Name)
	}
	return names
}

func TestOutputs(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src string
		want      []string
	}{
		{"flake-utils", "{ outputs = { flake-utils, ... }: flake-utils.lib.eachDefaultSystem (system: { packages.a = 1; devShells = { b = 1; }; }); }", []string{"packages.a", "devShells.b"}},
		{"named systems", "{ outputs = _: { packages.x86_64-linux.a = 1; packages.aarch64-linux.a = 1; apps.x86_64-linux = { b = 1; }; checks.x86_64-linux.c = 1; }; }", []string{"packages.a", "apps.b", "checks"}},
		{"antiquoted system", "{ outputs = _: { packages.${system}.a = 1; }; }", []string{"packages.a"}},
		{"merged sets", "{ outputs = _: { packages = f (s: { a = 1; }) // f (s: { b = 1; }); }; }", []string{"packages.a", "packages.b"}},
		{"perSystem path", "{ perSystem.packages.a = 1; }", []string{"packages.a"}},
		{"perSystem module", "{ ... }: { perSystem = { pkgs, ... }: { packages.a = 1; checks = { }; }; }", []string{"packages.a", "checks"}},
		{"perSystem inherit", "{ perSystem = { packages = { inherit a b; }; }; }", []string{"packages.a", "packages.b"}},
		{"mkFlake", "{ outputs = inputs: inputs.flake-parts.lib.mkFlake { inherit inputs; } { perSystem = { ... }: { apps.a = 1; }; flake.packages.x86_64-linux.b = 1; }; }", []string{"apps.a", "packages.b"}},
		{"not a flake or module", "{ packages.a = 1; }", nil},
		{"a name no flake reference can carry", "{ perSystem.packages.\"a b\" = 1; }", nil},
		{"an unknown system layer", "{ outputs = _: { packages.a = 1; }; }", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := outputNames(tt.src); !slices.Equal(got, tt.want) {
				t.Errorf("outputs = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOutputsOfTheCorpus(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		path string
		want []string
	}{
		{"single-flake/flake.nix", []string{"packages.default", "packages.logship", "packages.logship-rotate", "packages.manual", "apps.default", "checks", "devShells.default"}},
		{"flake-parts/flake.nix", nil},
		{"flake-parts/nix/packages/default.nix", []string{"packages.default", "packages.mirrorctl", "packages.platform-note"}},
		{"flake-parts/nix/packages/manual.nix", []string{"packages.manual"}},
		{"flake-parts/nix/checks.nix", []string{"checks"}},
		{"flake-parts/nix/shell.nix", []string{"devShells.default"}},
		{"flake-parts/nix/overlay.nix", nil},
		{"flake-parts/nix/project.nix", nil},
	} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", "corpus", "nix", filepath.FromSlash(tt.path)))
		if err != nil {
			t.Fatal(err)
		}
		if got := outputNames(string(raw)); !slices.Equal(got, tt.want) {
			t.Errorf("outputs of %s = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestInvocation(t *testing.T) {
	t.Parallel()

	for name, want := range map[string][]string{
		"packages.a":  {"nix", "build", ".#a"},
		"apps.a":      {"nix", "run", ".#a"},
		"devShells.a": {"nix", "develop", ".#a", "--command", "true"},
		"checks":      {"nix", "flake", "check"},
		"inputs.a":    {"nix", "flake", "update", "a"},
	} {
		if got := Invocation(name); !slices.Equal(got, want) {
			t.Errorf("Invocation(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestOnlyADevShellNeedsATerminal(t *testing.T) {
	t.Parallel()

	if got := TerminalInvocation("devShells.a"); !slices.Equal(got, []string{"nix", "develop", ".#a"}) {
		t.Errorf("TerminalInvocation = %q", got)
	}
	if !NeedsTerminal("devShells.a") || NeedsTerminal("packages.a") {
		t.Error("NeedsTerminal should hold for dev shells only")
	}
	if got := UnattendedTitle("devShells.a"); got != "Build dev shell .#a" {
		t.Errorf("UnattendedTitle = %q", got)
	}
	if got := UnattendedTitle("packages.a"); got != "nix build .#a" {
		t.Errorf("UnattendedTitle = %q", got)
	}
}
