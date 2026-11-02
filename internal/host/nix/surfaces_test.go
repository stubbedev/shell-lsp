package nix

import (
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/projection"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func TestSurfacesAreShell(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, nix, want string
		shell           dialect.Dialect
	}{
		{"zsh init through mkOrder", "{ programs.zsh.initContent = lib.mkOrder 550 ''\n  setopt autocd\n''; }", "setopt autocd", dialect.Zsh},
		{"zsh written as nested sets", "{ programs = { zsh = { envExtra = ''\n  x=1\n''; }; }; }", "x=1", dialect.Zsh},
		{"zsh under mkMerge", "{ programs.zsh.initContent = lib.mkMerge [ (lib.mkBefore ''\n  one\n'') ''\n  two\n'' ]; }", "two", dialect.Zsh},
		{"bash init", "{ programs.bash.bashrcExtra = ''\n  shopt -s globstar\n''; }", "shopt -s globstar", dialect.Bash},
		{"an alias value", "{ home.shellAliases.ll = \"ls -l\"; }", "ls -l", dialect.Bash},
		{"a zsh alias value", "{ programs.zsh.shellAliases.ll = \"ls -l\"; }", "ls -l", dialect.Zsh},
		{"home activation through a DAG entry", "{ home.activation.x = lib.hm.dag.entryAfter [ \"writeBoundary\" ] ''\n  run ls\n''; }", "run ls", dialect.Bash},
		{"a git alias that runs a shell", "{ programs.git.settings.alias.st = \"!git status\"; }", "git status", dialect.Posix},
		{"an activation script", "{ system.activationScripts.x = lib.stringAfter [ \"users\" ] ''\n  mkdir -p /x\n''; }", "mkdir -p /x", dialect.Bash},
		{"an activation script with text", "{ system.activationScripts.x.text = ''\n  mkdir -p /x\n''; }", "mkdir -p /x", dialect.Bash},
		{"environment init", "{ environment.interactiveShellInit = ''\n  umask 22\n''; }", "umask 22", dialect.Bash},
		{"initrd commands", "{ boot.initrd.postMountCommands = ''\n  mount /x\n''; }", "mount /x", dialect.Busybox},
		{"a user unit", "{ systemd.user.services.x.preStart = ''\n  mkdir /y\n''; }", "mkdir /y", dialect.Bash},
		{"an initrd unit", "{ boot.initrd.systemd.services.x.script = ''\n  mkdir /y\n''; }", "mkdir /y", dialect.Bash},
		{"a unit made by a function", "{ systemd.services = lib.mapAttrs (name: cfg: { script = ''\n  mkdir /y\n''; }) units; }", "mkdir /y", dialect.Bash},
		{"a module behind a function", "{ flake.modules.homeManager.x = { config, ... }: { programs.bash.initExtra = ''\n  y=1\n''; }; }", "y=1", dialect.Bash},
		{"a unit command line", "{ systemd.services.x.serviceConfig.ExecStart = \"-${pkg}/bin/foo --flag\"; }", "foo --flag", dialect.Posix},
		{"unit command lines in a list", "{ systemd.services.x.serviceConfig.ExecStartPre = [ \"one\" \"two\" ]; }", "two", dialect.Posix},
		{"a home manager unit command line", "{ systemd.user.services.x.Service.ExecStart = \"foo --flag\"; }", "foo --flag", dialect.Posix},
		{"a bash writer with options", "writers.writeBash \"x\" { makeWrapperArgs = [ ]; } ''\n  echo a\n''", "echo a", dialect.Bash},
		{"a dash writer", "writeDashBin \"x\" ''\n  echo a\n''", "echo a", dialect.Dash},
		{"runCommandWith", "runCommandWith { name = \"x\"; } ''\n  echo a > $out\n''", "echo a > $out", dialect.Bash},
		{"a file named by writeText", "writeText \"a.sh\" ''\n  echo a\n''", "echo a", dialect.Posix},
		{"a file below home", "{ home.file.\"bin/a.bash\".text = ''\n  echo a\n''; }", "echo a", dialect.Bash},
		{"a file below xdg", "{ xdg.configFile.\"x/y.zsh\".text = ''\n  print a\n''; }", "print a", dialect.Zsh},
		{"a file below etc", "{ environment.etc.\"a.sh\".text = ''\n  echo a\n''; }", "echo a", dialect.Posix},
		{"a target decides the file name", "{ home.file.conf = { target = \"a.zsh\"; text = ''\n  print a\n''; }; }", "print a", dialect.Zsh},
		{"a shebang decides over a file name", "{ home.file.\"bin/x\".text = ''\n  #!/usr/bin/env dash\n  echo a\n''; }", "echo a", dialect.Dash},
		{"writeTextFile", "writeTextFile { name = \"x\"; destination = \"/bin/a.zsh\"; text = ''\n  print a\n''; }", "print a", dialect.Zsh},
		{"an image script", "dockerTools.buildImage { name = \"x\"; runAsRoot = ''\n  mkdir /x\n''; }", "mkdir /x", dialect.Bash},
		{"a layered image script", "dockerTools.buildLayeredImage { name = \"x\"; fakeRootCommands = ''\n  mkdir /x\n''; }", "mkdir /x", dialect.Bash},
		{"an FHS profile", "buildFHSEnv { name = \"x\"; profile = ''\n  export A=1\n''; }", "export A=1", dialect.Bash},
		{"a writer inside a string that is no script", "{ a = \"${writeShellScript \"x\" ''\n  echo a\n''}\"; }", "echo a", dialect.Bash},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := Analyze(tt.nix)
			if !strings.Contains(a.Map.Text(), tt.want) {
				t.Fatalf("projection = %q, want it to hold %q", a.Map.Text(), tt.want)
			}
			shells := a.Map.Shells()
			if !slices.ContainsFunc(shells, func(d dialect.Detection) bool { return d.Dialect == tt.shell }) {
				t.Errorf("shells = %+v, want %s", shells, tt.shell)
			}
		})
	}
}

func TestSurfacesThatAreNoShell(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, nix string }{
		{"a fish writer", "writers.writeFish \"x\" ''\n  echo a\n''"},
		{"a nushell writer", "writers.writeNuBin \"x\" ''\n  echo a\n''"},
		{"a python writer", "writers.writePython3 \"x\" { } ''\n  print(1)\n''"},
		{"fish init", "{ programs.fish.interactiveShellInit = ''\n  set -x A 1\n''; }"},
		{"nushell init", "{ programs.nushell.extraConfig = ''\n  echo a\n''; }"},
		{"a NixOS test", "nixosTest { name = \"x\"; testScript = ''\n  start_all()\n''; }"},
		{"udev rules", "{ services.udev.extraRules = ''\n  RUN+=\"/bin/x\"\n''; }"},
		{"a git alias that runs git", "{ programs.git.settings.alias.st = \"status\"; }"},
		{"a file that is not a script", "{ xdg.configFile.\"a.toml\".text = ''\n  x = 1\n''; }"},
		{"a script in another language", "{ home.file.\"bin/x\".text = ''\n  #!/usr/bin/env python3\n  print(1)\n''; }"},
		{"a bare text", "{ text = ''\n  echo a\n''; }"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Analyze(tt.nix).Map.Text(); got != "" {
				t.Errorf("projection = %q", got)
			}
		})
	}
}

func TestCommandWordsAreNamedForTheProgram(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, script, want string }{
		{"a bin path", "${pkgs.jq}/bin/jq . f", "jq . f"},
		{"a quoted bin path", "\"${pkgs.jq}/bin/jq\" . f", "\"jq\" . f"},
		{"getExe", "${lib.getExe pkgs.gawk} '{ print }' f", "gawk '{ print }' f"},
		{"getExe with a main program of its own", "${lib.getExe pkgs.ripgrep} x", "rg x"},
		{"getExe' names the program", "${lib.getExe' pkgs.coreutils \"ls\"} -l", "ls -l"},
		{"behind a wrapper", "sudo -n ${pkgs.jq}/bin/jq . f", "sudo -n jq . f"},
		{"in a command substitution", "x=$(${pkgs.jq}/bin/jq . f)", "x=$(jq . f)"},
		{"not as an argument", "ls ${pkgs.jq}/bin/jq", "ls $$/bin/jq"},
		{"not a package itself", "${tool} x", "$$ x"},
		{"not any other path", "${pkgs.jq}/share/jq x", "$$/share/jq x"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Analyze("{ buildPhase = ''\n  " + tt.script + "\n''; }").Map.Text()
			if !strings.Contains(got, "\n"+tt.want+"\n") {
				t.Errorf("projection = %q, want it to hold %q", got, tt.want)
			}
		})
	}
}

func TestCommandWordsMapBackToTheFile(t *testing.T) {
	t.Parallel()

	src := "{ buildPhase = ''\n  ${pkgs.jq}/bin/jq . f\n''; }"
	m := Analyze(src).Map
	at := strings.Index(m.Text(), "jq . f")
	if got, want := int(m.HostOffset(projection.ProjOffset(at))), strings.Index(src, "jq . f"); got != want {
		t.Errorf("the program starts at %d, want %d", got, want)
	}
	if _, inside := m.Offset(projection.HostOffset(strings.Index(src, "pkgs"))); inside {
		t.Errorf("the antiquotation reads as shell")
	}
	if _, inside := m.Offset(projection.HostOffset(strings.Index(src, "bin/"))); !inside {
		t.Errorf("the path next to the antiquotation does not read as shell")
	}
	if _, ok := m.Edit(textedit.Edit{Start: at, End: at + len("jq"), NewText: "x"}); ok {
		t.Errorf("an edit of the program name was accepted")
	}
}

func TestAntiquotationsThatHoldShell(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, script, want string }{
		{"optionalString", "echo a\n  ${lib.optionalString debug ''\n    set -x\n  ''}\n  echo b", "echo a\nset -x\n\necho b"},
		{"concatStringsSep", "${lib.concatStringsSep \" \" [ \"a\" \"b\" ]}", "a b"},
		{"concatMapStrings", "${lib.concatMapStrings (x: ''\n    echo ${x}\n  '') xs}", "echo $$\n"},
		{"concatMapStringsSep", "${lib.concatMapStringsSep \"\\n\" (x: \"echo ${x}\") xs}", "echo $$"},
		{"a branch", "${if debug then \"set -x\" else \"\"}", "set -x"},
		{"the other branch", "${if debug then \"\" else \"set -x\"}", "set -x"},
		{"a nested writer runs apart", "${writeShellScript \"x\" ''\n    echo a\n  ''} now", "$(\necho a\n\n) now"},
		{"an escaped word stays a word", "${lib.escapeShellArg x}", "$$"},
		{"a text in single quotes stays a word", "echo '${lib.optionalString debug \"a\"}'", "''$$''"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Analyze("{ buildPhase = ''\n  " + tt.script + "\n''; }").Map.Text()
			if !strings.Contains(got, tt.want) {
				t.Errorf("projection = %q, want it to hold %q", got, tt.want)
			}
		})
	}
}

func TestShellInASplicedStringIsShell(t *testing.T) {
	t.Parallel()

	src := "{ buildPhase = ''\n  ${lib.optionalString debug ''\n    echo $a\n  ''}\n''; }"
	a := Analyze(src)
	inner := strings.Index(src, "$a")
	if !slices.ContainsFunc(a.Index.Shells, func(s model.Shell) bool { return s.Span.Start <= inner && inner < s.Span.End }) {
		t.Errorf("shells = %+v do not hold the spliced string", a.Index.Shells)
	}
}

func TestUnescapedAntiquotations(t *testing.T) {
	t.Parallel()

	const code = "SL8014"
	for _, tt := range []struct {
		name, nix string
		want      []string
	}{
		{"an unknown value", "x: { buildPhase = ''\n  echo ${x}\n''; }", []string{code + " ${x}"}},
		{"inside double quotes", "x: { buildPhase = ''\n  echo \"${x}\"\n''; }", []string{code + " ${x}"}},
		{"an attribute of a config", "config: { buildPhase = ''\n  echo ${config.a}\n''; }", []string{code + " ${config.a}"}},
		{"in a spliced string", "x: { buildPhase = ''\n  ${lib.optionalString c ''\n    echo ${x}\n  ''}\n''; }", []string{code + " ${x}"}},
		{"escaped", "lib: x: { buildPhase = ''\n  echo ${lib.escapeShellArg x}\n''; }", nil},
		{"escaped through toString", "lib: x: { buildPhase = ''\n  echo ${lib.escapeShellArg (toString x)}\n''; }", nil},
		{"every item escaped", "lib: x: { buildPhase = ''\n  echo ${lib.concatMapStringsSep \" \" lib.escapeShellArg x}\n''; }", nil},
		{"a package", "pkgs: { buildPhase = ''\n  cat ${pkgs.jq}\n''; }", nil},
		{"a path", "{ buildPhase = ''\n  cat ${./x}\n''; }", nil},
		{"a path below a value", "x: { buildPhase = ''\n  cat ${x}/y\n''; }", nil},
		{"a literal", "let a = \"1.0\"; in { buildPhase = ''\n  echo ${a}\n''; }", nil},
		{"a number", "let a = 1; in { buildPhase = ''\n  echo ${toString a}\n''; }", nil},
		{"a literal with a space", "let a = \"x y\"; in { buildPhase = ''\n  echo ${a}\n''; }", []string{code + " ${a}"}},
		{"a script", "writeShellScript: let a = writeShellScript \"s\" ''\n  :\n''; in { buildPhase = ''\n  ${a}\n''; }", nil},
		{"a file", "let a = writeText \"s\" \"x\"; in { buildPhase = ''\n  cat ${a}\n''; }", nil},
		{"a comment", "x: { buildPhase = ''\n  # ${x}\n''; }", nil},
		{"a here-document", "x: { buildPhase = ''\n  cat <<EOF\n  ${x}\n  EOF\n''; }", nil},
		{"a plain string", "x: { a = \"${x}\"; }", nil},
		{"a whole statement", "x: { buildPhase = ''\n  ${x}\n''; }", nil},
		{"a command word", "x: { buildPhase = ''\n  ${x} --flag\n''; }", nil},
		{"a command under a wrapper", "x: { buildPhase = ''\n  exec ${x} --flag\n''; }", nil},
		{"an assignment value", "x: { buildPhase = ''\n  y=${x}\n''; }", nil},
		{"a loop list", "x: { buildPhase = ''\n  for i in ${x}; do :; done\n''; }", nil},
		{"an argument in a pipeline", "x: { buildPhase = ''\n  echo a | tee ${x}\n''; }", []string{code + " ${x}"}},
		{"after an apostrophe in a comment", "x: { buildPhase = ''\n  # don't\n  echo ${x}\n''; }", []string{code + " ${x}"}},
		{"a comment hash inside a word is no comment", "x: { buildPhase = ''\n  echo a#b ${x}\n''; }", []string{code + " ${x}"}},
		{"getExe", "lib: pkgs: { buildPhase = ''\n  time ${lib.getExe pkgs.jq} .\n''; }", nil},
		{"a function building from its argument", "pkgs: let f = n: \"${pkgs.jq}/${n}\"; a = f \"x\"; in { buildPhase = ''\n  source ${a}\n''; }", []string{code + " ${a}"}},
		{"a function returning a store path", "pkgs: let f = n: \"${pkgs.runCommand n { } ''\n  echo hi\n''}/init\"; a = f \"x\"; in { buildPhase = ''\n  source ${a}\n''; }", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, s := range summaries(tt.nix, UnescapedAntiquotationFindings(tt.nix)) {
				if strings.HasPrefix(s, code) {
					got = append(got, s)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("findings = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSurfaceTableIsConsistent(t *testing.T) {
	t.Parallel()

	for _, s := range surfaceTable() {
		if (s.attr == "") == (s.call == "") {
			t.Errorf("surface %+v names neither or both of an attribute and a call", s)
		}
		if s.role == foreignRole && s.language == "" && s.call != "writeCBin" && s.key == "" {
			t.Errorf("surface %+v is foreign without saying what it is", s)
		}
	}
}

func TestSourcedSurfacesKeepWhatTheyDefine(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, nix string
		want      string
	}{
		{"a zsh init", "{ programs.zsh.initContent = ''\n  f() { :; }\n  X=1\n''; }", sourcedDirective},
		{"a bash init", "{ programs.bash.initExtra = ''\n  f() { :; }\n''; }", sourcedDirective},
		{"environment", "{ environment.shellInit = ''\n  X=1\n''; }", sourcedDirective},
		{"an activation entry", "{ home.activation.a = lib.hm.dag.entryAfter [ \"writeBoundary\" ] ''\n  $DRY_RUN_CMD mkdir $VERBOSE_ARG x\n''; }", sourcedDirective},
		{"a marked fragment", "{ a = /* zsh */ ''\n  f() { :; }\n''; }", sourcedDirective},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Analyze(tt.nix).Map.Text(); !strings.HasPrefix(got, tt.want) {
				t.Errorf("projection %q does not open with %q", got, tt.want)
			}
		})
	}
	activation := Analyze("{ home.activation.a = ''\n  $DRY_RUN_CMD mkdir $VERBOSE_ARG x\n''; }").Map.Text()
	if !strings.Contains(activation, "DRY_RUN_CMD=x\n") || !strings.Contains(activation, "VERBOSE_ARG=x\n") {
		t.Errorf("activation projection %q does not assign what Home Manager provides", activation)
	}
	marked := Analyze("writeShellApplication { name = \"a\"; text = /* bash */ ''\n  echo hi\n''; }").Map.Text()
	if !strings.Contains(marked, strictMode) {
		t.Errorf("a marker on a known writer dropped the writer's shape: %q", marked)
	}
}

func TestPhasesProjectOnlyWhatTheyRead(t *testing.T) {
	t.Parallel()

	text := Analyze("{ outputs = [ \"out\" \"dev\" ]; installPhase = ''\n  cp a $out/x $dev ''${TMPDIR}/y\n  z=$unknown\n''; }").Map.Text()
	for _, want := range []string{"TMPDIR=x\n", "dev=x\n", "out=x\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("projection %q lacks %q", text, want)
		}
	}
	if strings.Contains(text, "unknown=x") || strings.Contains(text, "pname=x") {
		t.Errorf("projection %q assigns a variable it should not", text)
	}
}
