package nix

import (
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/nixeval"
)

type fakeEvaluated struct {
	sites []OptionSite
	roots map[string][]string
}

func (f *fakeEvaluated) OptionChildren(site OptionSite) []OptionChild {
	f.sites = append(f.sites, site)
	return []OptionChild{
		{Name: "enable", Sources: []string{"NixOS configuration box"}, Option: &nixeval.Option{Path: append(slices.Clone(site.Path), "enable"), Type: "boolean", Description: "Turns it on.", Default: "false"}},
		{Name: "dash-free name", Sources: []string{"NixOS configuration box", "flake-parts evaluation"}},
	}
}

func (f *fakeEvaluated) OptionAt(site OptionSite) (SourcedOption, bool) {
	f.sites = append(f.sites, site)
	return SourcedOption{
		Option:  nixeval.Option{Path: site.Path, Type: "boolean", Description: "Turns it on.", Default: "false", Example: "true", Declarations: []nixeval.Declaration{{File: "/modules/x.nix", Line: 7}}},
		Sources: []string{"NixOS configuration box"},
	}, true
}

func (f *fakeEvaluated) NixpkgsChildren(path []string, prefix string) []string {
	f.roots[strings.Join(path, ".")+"|"+prefix] = nil
	return []string{prefix + "hello", prefix + "world"}
}

func (f *fakeEvaluated) NixpkgsPackage(path []string) (*nixeval.Package, bool) {
	return &nixeval.Package{Name: "hello-2.0", Version: "2.0", Description: "Says hello.", Declaration: nixeval.Declaration{File: "/pkgs/" + strings.Join(path, "/") + ".nix", Line: 3}}, true
}

func newFakeEvaluated() *fakeEvaluated { return &fakeEvaluated{roots: map[string][]string{}} }

func TestOptionSitesFollowTheModuleLayout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		src   string
		class OptionClass
		path  []string
	}{
		{"module head", "{ config, ... }: { services.nginx.| }", ClassUnknown, []string{"services", "nginx"}},
		{"plain set", "{ home.| }", ClassUnknown, []string{"home"}},
		{"under config", "{ config.services.| }", ClassUnknown, []string{"services"}},
		{"nested sets", "{ services = { nginx = { | }; }; }", ClassUnknown, []string{"services", "nginx"}},
		{"through mkIf", "{ lib, ... }: { config = lib.mkIf true { services.| }; }", ClassUnknown, []string{"services"}},
		{"through mkMerge", "{ lib, ... }: { config = lib.mkMerge [ { services.| } ]; }", ClassUnknown, []string{"services"}},
		{"inline import", "{ imports = [ { services.| } ]; }", ClassUnknown, []string{"services"}},
		{"perSystem", "{ perSystem = { pkgs, ... }: { packages.| }; }", ClassFlakeParts, []string{"perSystem", "packages"}},
		{"perSystem head", "{ perSystem.| }", ClassFlakeParts, []string{"perSystem"}},
		{"dendritic nixos", "{ flake.modules.nixos.web = { services.| }; }", ClassNixOS, []string{"services"}},
		{"dendritic home manager", "{ flake.modules.homeManager.web = { config, ... }: { home.| }; }", ClassHomeManager, []string{"home"}},
		{"dendritic nested", "{ flake = { modules = { nixos = { web = { | }; }; }; }; }", ClassNixOS, nil},
		{"dendritic other class", "{ flake.modules.generic.web = { | }; }", ClassUnknown, nil},
		{"flake output module", "{ flake.nixosModules.web = { services.| }; }", ClassNixOS, []string{"services"}},
		{"plain flake output", "{ outputs = { self }: { nixosModules.web = { services.| }; }; }", ClassNixOS, []string{"services"}},
		{"nixosSystem module", "{ outputs = { nixpkgs }: { nixosConfigurations.a = nixpkgs.lib.nixosSystem { modules = [ { services.| } ]; }; }; }", ClassNixOS, []string{"services"}},
		{"home manager user", "{ home-manager.users.me = { home.| }; }", ClassHomeManager, []string{"home"}},
		{"mkFlake argument", "{ outputs = inputs: inputs.parts.lib.mkFlake { inherit inputs; } { perSystem.| }; }", ClassFlakeParts, []string{"perSystem"}},
		{"config select", "{ config, ... }: { x = config.services.nginx.|; }", ClassUnknown, []string{"services", "nginx"}},
		{"config through let", "{ config, ... }: let cfg = config.services.nginx; in { x = cfg.|; }", ClassUnknown, []string{"services", "nginx"}},
		{"config in perSystem", "{ perSystem = { config, ... }: { x = config.packages.|; }; }", ClassFlakeParts, []string{"perSystem", "packages"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			text, at := marked(t, tt.src)
			known := newFakeEvaluated()
			items := Complete(text, at, NoFiles, known)
			if len(known.sites) != 1 || known.sites[0].Class != tt.class || !slices.Equal(known.sites[0].Path, tt.path) {
				t.Fatalf("asked for sites %+v, want class %d path %q", known.sites, tt.class, tt.path)
			}
			if !slices.Contains(labelsOf(items), "enable") || !slices.Contains(labelsOf(items), `"dash-free name"`) {
				t.Errorf("items = %q", labelsOf(items))
			}
		})
	}
}

func TestNoOptionsWhereNoOptionCanBeSet(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		"{ options.services.| }",
		"{ lib, stdenv }: stdenv.mkDerivation { pname.| }",
		"{ outputs = { self }: { packages.| }; }",
		"{ inputs.nixpkgs.| ; outputs = _: { }; }",
		"let x = { services.| }; in x",
		"{ | }: 1",
		"let x = { | }; in x",
		"{ options = { | }; }",
		"{ outputs = { self }: { packages = { | }; }; }",
	} {
		text, at := marked(t, src)
		known := newFakeEvaluated()
		Complete(text, at, NoFiles, known)
		if len(known.sites) != 0 {
			t.Errorf("%q asked for option sites %+v", src, known.sites)
		}
	}
}

func TestNixpkgsCompletionAtItsPlugPoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src, query string
	}{
		{"{ pkgs, ... }: pkgs.|", "|"},
		{"{ pkgs, ... }: pkgs.he|", "|he"},
		{"{ pkgs, ... }: pkgs.python3Packages.|", "python3Packages|"},
		{"{ lib, ... }: lib.|", "lib|"},
		{"{ lib, ... }: lib.strings.|", "lib.strings|"},
		{"{ pkgs, ... }: with pkgs; [ | ]", "|"},
		{"{ pkgs, ... }: with pkgs.python3Packages; [ | ]", "python3Packages|"},
		{"{ pkgs, ... }: { inherit (pkgs) | ; }", "|"},
		{"let pkgs = import ./n.nix { }; in pkgs.|", "|"},
	}
	for _, tt := range tests {
		text, at := marked(t, tt.src)
		known := newFakeEvaluated()
		Complete(text, at, NoFiles, known)
		if _, asked := known.roots[tt.query]; !asked {
			t.Errorf("%q asked nixpkgs for %q, want %q", tt.src, known.roots, tt.query)
		}
	}
}

func TestOtherSelectsStayStatic(t *testing.T) {
	t.Parallel()

	text, at := marked(t, "let pkgs = { own = 1; }; in pkgs.|")
	known := newFakeEvaluated()
	if got := labelsOf(Complete(text, at, NoFiles, known)); !slices.Equal(got, []string{"own"}) || len(known.roots) != 0 {
		t.Errorf("a statically known pkgs completes as %q and asks %q", got, known.roots)
	}
}

func TestPackageDocIsResolvedLazily(t *testing.T) {
	t.Parallel()

	text, at := marked(t, "{ pkgs, ... }: pkgs.|")
	known := newFakeEvaluated()
	items := Complete(text, at, NoFiles, known)
	if len(items) == 0 || !items[0].HasDoc() || items[0].Doc != "" {
		t.Fatalf("items = %+v, want documentation left for resolve", items)
	}
	doc, ok := ItemDoc(text, at, "hello", NoFiles, known)
	if !ok || !strings.Contains(doc, "Says hello.") || !strings.Contains(doc, "/pkgs/hello.nix:3") {
		t.Errorf("ItemDoc = %q, %t", doc, ok)
	}
}

func TestEvaluatedHover(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src  string
		want []string
	}{
		{"{ services.nginx.en|able = true; }", []string{"services.nginx.enable : boolean", "Turns it on.", "Default: `false`", "Example: `true`", "`/modules/x.nix:7`", "From NixOS configuration box"}},
		{"{ flake.modules.nixos.web = { services.en|able = true; }; }", []string{"services.enable : boolean"}},
		{"{ perSystem = { ... }: { pack|ages = { }; }; }", []string{"perSystem.packages : boolean"}},
		{"{ config, ... }: { x = config.services.en|able; }", []string{"services.enable : boolean"}},
		{"{ pkgs, ... }: pkgs.hel|lo", []string{"pkgs.hello", "hello-2.0", "Says hello.", "/pkgs/hello.nix:3"}},
		{"{ pkgs, ... }: with pkgs; [ hel|lo ]", []string{"pkgs.hello", "Says hello."}},
		{"{ pkgs, ... }: { inherit (pkgs) hel|lo; }", []string{"pkgs.hello", "Says hello."}},
	}
	for _, tt := range tests {
		text, at := marked(t, tt.src)
		got, ok := Read(text).Hover(at, newFakeEvaluated())
		if !ok {
			t.Errorf("%q has no hover", tt.src)
			continue
		}
		for _, want := range tt.want {
			if !strings.Contains(got.Markdown, want) {
				t.Errorf("%q hovers as %q, want %q in it", tt.src, got.Markdown, want)
			}
		}
	}
}

func TestStaticHoverWinsOverEvaluation(t *testing.T) {
	t.Parallel()

	text, at := marked(t, "builtins.ma|p")
	known := newFakeEvaluated()
	got, ok := Read(text).Hover(at, known)
	if !ok || strings.Contains(got.Markdown, "Turns it on") || len(known.sites) != 0 {
		t.Errorf("hover = %+v, %t, sites %+v", got, ok, known.sites)
	}
}

func TestOptionMarkdownFencesValuesThatDoNotFitInline(t *testing.T) {
	t.Parallel()

	got := optionMarkdown(SourcedOption{Option: nixeval.Option{Path: []string{"a"}, Default: "[\n  1\n]"}})
	if !strings.Contains(got, "Default:\n\n```nix\n[\n  1\n]\n```") {
		t.Errorf("optionMarkdown = %q", got)
	}
}
