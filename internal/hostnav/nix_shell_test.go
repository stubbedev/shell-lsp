package hostnav

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/navigation"
)

func (tr *tree) shellDefinition(name, sub string) []string {
	tr.t.Helper()
	g, sym := tr.shellAt(name, sub, 0)
	return tr.locations(g.ShellDefinition(sym))
}

func TestNixToShellAcrossTheBoundary(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		files map[string]string
		in    string
		sub   string
		want  []string
	}{
		{
			name:  "an attribute merged in from another file",
			files: map[string]string{"default.nix": "let\n  common = import ./env.nix;\nin\n  stdenv.mkDerivation (common // {\n    buildPhase = ''\n      echo $shared\n    '';\n  })\n", "env.nix": "{\n  shared = \"1\";\n}\n"},
			in:    "default.nix", sub: "$shared", want: []string{"env.nix:2:shared"},
		},
		{
			name:  "an env set from another file",
			files: map[string]string{"default.nix": "stdenv.mkDerivation {\n  env = import ./env.nix;\n  buildPhase = ''\n    echo $shared\n  '';\n}\n", "env.nix": "{\n  shared = \"1\";\n}\n"},
			in:    "default.nix", sub: "$shared", want: []string{"env.nix:2:shared"},
		},
		{
			name:  "an inherit leads on to what it reads",
			files: map[string]string{"default.nix": "{ owner }:\nstdenv.mkDerivation {\n  inherit owner;\n  buildPhase = ''\n    echo $owner\n  '';\n}\n"},
			in:    "default.nix", sub: "$owner", want: []string{"default.nix:1:owner"},
		},
		{
			name:  "env.x",
			files: map[string]string{"default.nix": "stdenv.mkDerivation {\n  env.CFLAGS = \"-O2\";\n  buildPhase = ''\n    echo $CFLAGS\n  '';\n}\n"},
			in:    "default.nix", sub: "$CFLAGS", want: []string{"default.nix:2:CFLAGS"},
		},
		{
			name:  "passAsFile adds Path to the name",
			files: map[string]string{"default.nix": "stdenv.mkDerivation {\n  big = \"x\";\n  passAsFile = [ \"big\" ];\n  buildPhase = ''\n    cat $bigPath\n  '';\n}\n"},
			in:    "default.nix", sub: "$bigPath", want: []string{"default.nix:2:big"},
		},
		{
			name:  "makeWrapperArgs --set",
			files: map[string]string{"default.nix": "stdenv.mkDerivation {\n  makeWrapperArgs = [ \"--set\" \"MODE\" \"fast\" ];\n  postFixup = ''\n    echo $MODE\n  '';\n}\n"},
			in:    "default.nix", sub: "$MODE", want: []string{"default.nix:2:MODE"},
		},
		{
			name:  "a unit environment spelled in one path",
			files: map[string]string{"default.nix": "{\n  systemd.services.x.environment.PORT = \"1\";\n  systemd.services.x.script = ''\n    echo $PORT\n  '';\n}\n"},
			in:    "default.nix", sub: "$PORT", want: []string{"default.nix:2:PORT"},
		},
		{
			name: "environment.variables reaches a script of the same class",
			files: map[string]string{
				"default.nix": "{ lib }:\nlib.nixosSystem {\n  modules = [ ./vars.nix ./unit.nix ];\n}\n",
				"vars.nix":    "{\n  environment.variables.GREETING = \"hi\";\n}\n",
				"unit.nix":    "{\n  systemd.services.x.script = ''\n    echo $GREETING\n  '';\n}\n",
			},
			in: "unit.nix", sub: "$GREETING", want: []string{"vars.nix:2:GREETING"},
		},
		{
			name: "home.sessionVariables reaches a script of the same class",
			files: map[string]string{
				"default.nix": "{ lib }:\nlib.homeManagerConfiguration {\n  modules = [ ./vars.nix ./hook.nix ];\n}\n",
				"vars.nix":    "{\n  home.sessionVariables.EDITOR = \"vi\";\n}\n",
				"hook.nix":    "{\n  systemd.services.x.script = ''\n    echo $EDITOR\n  '';\n}\n",
			},
			in: "hook.nix", sub: "$EDITOR", want: []string{"vars.nix:2:EDITOR"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			expectStrings(t, tt.name, newTree(t, tt.files).shellDefinition(tt.in, tt.sub), tt.want)
		})
	}
}

func TestNixExportedAttributeIsReadAcrossFiles(t *testing.T) {
	t.Parallel()

	tr := newTree(t, map[string]string{
		"default.nix": "(import ./drv.nix).pname\n",
		"drv.nix":     "{\n  pname = \"x\";\n  buildPhase = ''\n    echo $pname\n  '';\n}\n",
	})
	expectStrings(t, "from the reader", tr.definitionAfter("default.nix", ").", "pname"), []string{"drv.nix:2:pname"})
	g, sym := tr.at("drv.nix", "pname", 0)
	expectStrings(t, "from the attribute", tr.describe(References(g, sym, true)), []string{
		"default.nix:1:pname",
		"drv.nix:2:pname",
		"drv.nix:4:pname",
	})
}

func TestNixBuilderVariablesAreKnown(t *testing.T) {
	t.Parallel()

	tr := nixTree(t, "{\n  buildPhase = ''\n    mkdir $out\n  '';\n}\n")
	g, sym := tr.shellAt(nixFile, "$out", 0)
	if sym.Builtin == nil || sym.Defined() {
		t.Fatalf("$out = %+v", sym)
	}
	if md := g.Describe(sym).Markdown; !strings.Contains(md, "store path") {
		t.Errorf("hover = %q", md)
	}
}

func TestNixRuntimeInputsAreCommands(t *testing.T) {
	t.Parallel()

	src := "let\n  tool = writeShellApplication {\n    name = \"renamed\";\n    text = ''\n      echo tool\n    '';\n  };\nin\n  writeShellApplication {\n    name = \"x\";\n    runtimeInputs = [ tool jq ];\n    text = ''\n      renamed\n      tool\n      jq .\n    '';\n  }\n"
	tr := nixTree(t, src)
	expectStrings(t, "a script by its own name", tr.shellDefinition(nixFile, "enamed\n      tool"), []string{"default.nix:2:tool"})
	expectStrings(t, "a binding by its name", tr.shellDefinition(nixFile, "ool\n      jq"), []string{"default.nix:2:tool"})
	expectStrings(t, "a package nothing here defines", tr.shellDefinition(nixFile, "q ."), []string{"default.nix:11:jq"})
	g, sym := tr.shellAt(nixFile, "q .", 0)
	if md := g.Describe(sym).Markdown; !strings.Contains(md, "runtimeInputs") {
		t.Errorf("hover = %q", md)
	}
	if err := g.PrepareRename(sym); !errors.Is(err, navigation.ErrNotRenamable) {
		t.Errorf("renaming a program = %v", err)
	}
}

func TestNixAntiquotationNextToTheShell(t *testing.T) {
	t.Parallel()

	tr := nixTree(t, "let\n  pkg = callPackage ./x.nix { };\nin {\n  buildPhase = ''\n    ${pkg}/bin/foo --help\n  '';\n}\n")
	expectStrings(t, "inside the antiquotation", tr.definitionAfter(nixFile, "${", "pkg"), []string{"default.nix:2:pkg"})
	expectStrings(t, "on the shell token next to it", tr.shellDefinition(nixFile, "bin/foo"), []string{"default.nix:2:pkg"})
	g, sym := tr.shellAt(nixFile, "bin/foo", 0)
	if md := g.Describe(sym).Markdown; !strings.Contains(md, "`foo`") || !strings.Contains(md, "`pkg`") {
		t.Errorf("hover = %q", md)
	}
}

func TestNixScriptsAreCommands(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, script, use, read string }{
		{"a script by itself", "hello = writeShellScript \"hello\" ''\n    echo hi\n  '';", "${hello} now", "${"},
		{"getExe", "hello = writeShellApplication {\n    name = \"hello\";\n    text = ''\n      echo hi\n    '';\n  };", "${lib.getExe hello} now", "getExe "},
		{"a bin path", "hello = writeShellApplication {\n    name = \"hello\";\n    text = ''\n      echo hi\n    '';\n  };", "${hello}/bin/hello now", "${"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := "let\n  " + tt.script + "\n  caller = writeShellScript \"caller\" ''\n    " + tt.use + "\n  '';\nin\n  caller\n"
			tr := nixTree(t, src)
			expectStrings(t, "definition", tr.definitionAfter(nixFile, tt.read, "hello"), []string{"default.nix:2:hello"})
			g := tr.group(nixFile)
			at := tr.after(nixFile, tt.read, "hello")
			hover, ok := g.Hover(at)
			if !ok || !strings.Contains(hover.Markdown, "echo hi") {
				t.Errorf("hover = %+v, %v", hover, ok)
			}
			sym, ok := g.CallTarget(at)
			if !ok {
				t.Fatal("the script is not a call target")
			}
			incoming := g.Incoming(sym)
			if len(incoming) != 1 || incoming[0].Name != "caller" {
				t.Errorf("incoming calls = %+v", incoming)
			}
			caller, ok := g.CallTarget(tr.offset(nixFile, "caller", 0))
			if !ok {
				t.Fatal("the caller is not a call target")
			}
			if outgoing := g.Outgoing(caller); len(outgoing) != 1 || outgoing[0].Name != "hello" {
				t.Errorf("outgoing calls = %+v", outgoing)
			}
			refs := tr.describe(References(g, sym, true))
			if len(refs) != 2 || refs[0] != "default.nix:2:hello" {
				t.Errorf("references = %q", refs)
			}
		})
	}
}

const phasedDerivation = "stdenv.mkDerivation {\n  preBuild = ''\n    export FOO=1\n    helper() { :; }\n  '';\n  buildPhase = ''\n    helper\n    echo $FOO $LATE\n  '';\n  postInstall = ''\n    export LATE=1\n    echo $FOO\n  '';\n}\n"

func TestNixPhasesSeeEachOtherInOrder(t *testing.T) {
	t.Parallel()

	tr := nixTree(t, phasedDerivation)
	expectStrings(t, "an export of an earlier hook", tr.shellDefinition(nixFile, "$FOO $LATE"), []string{"default.nix:3:FOO"})
	expectStrings(t, "a function of an earlier hook", tr.shellDefinition(nixFile, "elper\n    echo"), []string{"default.nix:4:helper"})
	g, sym := tr.shellAt(nixFile, "$LATE", 0)
	if sym.Defined() || !strings.Contains(g.Describe(sym).Markdown, "postInstall") {
		t.Errorf("a variable set later = %+v: %q", sym, g.Describe(sym).Markdown)
	}
	g, sym = tr.shellAt(nixFile, "$FOO $LATE", 0)
	expectStrings(t, "every phase that can read it", tr.describe(References(g, sym, true)), []string{
		"default.nix:3:FOO",
		"default.nix:8:FOO",
		"default.nix:12:FOO",
	})
	occs, err := g.Rename(sym, "BAR")
	if err != nil || len(occs) != 3 {
		t.Errorf("rename = %+v, %v", occs, err)
	}
}

func TestNixPhasesOfAnotherDerivationDoNotReach(t *testing.T) {
	t.Parallel()

	tr := nixTree(t, "[\n  (stdenv.mkDerivation {\n    preBuild = ''\n      export FOO=1\n    '';\n  })\n  (stdenv.mkDerivation {\n    buildPhase = ''\n      echo $FOO\n    '';\n  })\n]\n")
	g, sym := tr.shellAt(nixFile, "$FOO", 0)
	if sym.Defined() || !strings.Contains(g.Describe(sym).Markdown, "other scripts") {
		t.Errorf("a variable of another derivation = %+v", sym)
	}
}

func TestNixInstallablesInTheShell(t *testing.T) {
	t.Parallel()

	tr := newTree(t, map[string]string{
		"flake.nix": "{\n  outputs = { self }: {\n    packages.x86_64-linux.tool = 1;\n    overlays.default = final: prev: {\n      extra = 1;\n    };\n    run = writeShellScript \"r\" ''\n      nix build .#tool\n      nix-shell -p extra hello\n    '';\n  };\n}\n",
	})
	expectStrings(t, "nix build .#x", tr.shellDefinition("flake.nix", "#tool"), []string{"flake.nix:3:tool"})
	expectStrings(t, "nix-shell -p", tr.shellDefinition("flake.nix", "xtra hello"), []string{"flake.nix:5:extra"})
}

func TestNixHeredocIsNix(t *testing.T) {
	t.Parallel()

	tr := nixTree(t, "writeShellScript \"gen\" ''\n  cat > out.nix <<EOF\n  let one = 1; in one\n  EOF\n''\n")
	expectStrings(t, "a read in the heredoc", tr.shellDefinition(nixFile, "ne\n  EOF"), []string{"default.nix:3:one"})
	g, sym := tr.shellAt(nixFile, "ne\n  EOF", 0)
	if err := g.PrepareRename(sym); !errors.Is(err, navigation.ErrNotRenamable) {
		t.Errorf("renaming inside a heredoc = %v", err)
	}
}

func TestNixDendriticWrappersCorpus(t *testing.T) {
	t.Parallel()

	tr := corpusTree(t, "dendritic")
	const probes = "modules/probes.nix"
	expectStrings(t, "pkgs through an overlay", tr.definitionAfter(probes, "pkgs.", "harbor"), []string{"modules/core/builders.nix:19:harbor"})
	expectStrings(t, "vars.x reaches $x", tr.definitionAfter(probes, "\"$", "expected"), []string{probes + ":5:expected"})
	g, sym := tr.at(probes, "expected", 0)
	expectStrings(t, "and back", tr.describe(References(g, sym, true)), []string{probes + ":5:expected", probes + ":7:expected"})

	g = tr.group(probes)
	shells := g.Module.Links().Shells(g.self)
	text := g.Self.Doc.Source().Text()
	var got []string
	for _, s := range shells {
		got = append(got, strings.TrimSpace(text[s.Span.Start:s.Span.End]))
	}
	want := []string{`test -e "$expected" && touch "$out"`, `touch "$out"`}
	if !slices.Equal(got, want) {
		t.Errorf("proven shell strings = %q, want %q", got, want)
	}
	builtin, ok := g.At(tr.offset(probes, "touch \"$out", 1) + len("ouch \"$"))
	if !ok || builtin.Builtin == nil {
		t.Errorf("$out in a wrapper that runs runCommand = %+v, %v", builtin, ok)
	}
}

func TestNixDiagnosticsStayOutOfWrappers(t *testing.T) {
	t.Parallel()

	tr := corpusTree(t, "dendritic")
	g := tr.group("modules/probes.nix")
	g.Findings()
	if len(g.Module.Files) != 1 {
		t.Errorf("diagnostics hold %d files", len(g.Module.Files))
	}
}

func TestNixRenameRefusesWhenTheScanWasCut(t *testing.T) {
	t.Parallel()

	files := map[string]string{"default.nix": "{ foo }: foo\n"}
	tr := newTree(t, files)
	file, err := tr.ws.Load(tr.path(nixFile))
	if err != nil {
		t.Fatal(err)
	}
	env := Env{Loader: tr.ws}
	env.Graph = func(context.Context) (*Graph, error) { return &Graph{importers: map[string][]importer{}, cut: true}, nil }
	g, err := Build(context.Background(), hostOf(t, file), env)
	if err != nil {
		t.Fatal(err)
	}
	sym, ok := g.At(tr.offset(nixFile, "foo", 0))
	if !ok {
		t.Fatal("no symbol")
	}
	err = g.PrepareRename(sym)
	if !errors.Is(err, navigation.ErrNotRenamable) || !strings.Contains(err.Error(), "scan") {
		t.Errorf("renaming a formal after a cut scan = %v", err)
	}
	if plain, unplain := tr2(t, files); plain.PrepareRename(unplain) != nil {
		t.Errorf("renaming the same formal without a cut: %v", plain.PrepareRename(unplain))
	}
}

func tr2(t *testing.T, files map[string]string) (*Group, Symbol) {
	t.Helper()
	return newTree(t, files).at(nixFile, "foo", 0)
}

func TestNixStageDGaps(t *testing.T) {
	t.Parallel()

	nixos := "{ lib }:\nlib.nixosSystem {\n  modules = [ ./options.nix ./reader.nix ];\n}\n"
	for _, tt := range []struct {
		name   string
		files  map[string]string
		in     string
		before string
		sub    string
		want   []string
	}{
		{
			name:  "a path spelled in a string",
			files: map[string]string{"default.nix": "(import \"${./dir}/y.nix\").foo\n", "dir/y.nix": "{\n  foo = 1;\n}\n"},
			in:    "default.nix", before: ").", sub: "foo", want: []string{"dir/y.nix:2:foo"},
		},
		{
			name:  "a path joined with +",
			files: map[string]string{"default.nix": "(import (./dir + \"/y.nix\")).foo\n", "dir/y.nix": "{\n  foo = 1;\n}\n"},
			in:    "default.nix", before: ")).", sub: "foo", want: []string{"dir/y.nix:2:foo"},
		},
		{
			name:  "an extensionless file",
			files: map[string]string{"default.nix": "(import ./values).foo\n", "values": "{\n  foo = 1;\n}\n"},
			in:    "default.nix", before: ").", sub: "foo", want: []string{"values:2:foo"},
		},
		{
			name: "a submodule option",
			files: map[string]string{
				"default.nix": nixos,
				"options.nix": "{ lib, ... }: {\n  options.svc = lib.mkOption {\n    type = lib.types.attrsOf (lib.types.submodule {\n      options.port = lib.mkOption { };\n    });\n  };\n}\n",
				"reader.nix":  "{ config, ... }: {\n  a = config.svc.web.port;\n}\n",
			},
			in: "reader.nix", before: "web.", sub: "port", want: []string{"options.nix:4:port"},
		},
		{
			name: "options made by mapAttrs",
			files: map[string]string{
				"default.nix": nixos,
				"options.nix": "{ lib, ... }: {\n  options.tools = lib.mapAttrs (_: _: lib.mkOption { }) {\n    jq = 1;\n  };\n}\n",
				"reader.nix":  "{ config, ... }: {\n  a = config.tools.jq;\n}\n",
			},
			in: "reader.nix", before: "tools.", sub: "jq", want: []string{"options.nix:3:jq"},
		},
		{
			name: "moduleWithSystem",
			files: map[string]string{
				"flake.nix": "{\n  outputs = inputs: inputs.flake-parts.lib.mkFlake { inherit inputs; } {\n    imports = [ ./m.nix ];\n  };\n}\n",
				"m.nix":     "{ moduleWithSystem, ... }: {\n  perSystem = {\n    packages.y = 1;\n  };\n  flake.nixosModules.x = moduleWithSystem ({ config }: { ... }: {\n    a = config.packages.y;\n  });\n}\n",
			},
			in: "m.nix", before: "config.packages.", sub: "y", want: []string{"m.nix:3:y"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			expectStrings(t, tt.name, newTree(t, tt.files).definitionAfter(tt.in, tt.before, tt.sub), tt.want)
		})
	}
}

func TestNixAFileInTwoClassesKeepsOneIdentity(t *testing.T) {
	t.Parallel()

	tr := newTree(t, map[string]string{
		"default.nix": "{ lib }: [\n  (lib.nixosSystem { modules = [ ./shared.nix ./sys.nix ]; })\n  (lib.homeManagerConfiguration { modules = [ ./shared.nix ./user.nix ]; })\n]\n",
		"shared.nix":  "{\n  flag = 1;\n}\n",
		"sys.nix":     "{\n  flag = 2;\n}\n",
		"user.nix":    "{ config, ... }: {\n  a = config.flag;\n}\n",
	})
	want := []string{"shared.nix:2:flag", "sys.nix:2:flag", "user.nix:2:flag"}
	for _, name := range []string{"shared.nix", "user.nix"} {
		g, sym := tr.at(name, "flag", 0)
		expectStrings(t, "references from "+name, tr.describe(References(g, sym, true)), want)
	}
}
