package filelint

import (
	"fmt"
	"maps"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/workspace"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

func nixFile(t testing.TB, text string) *workspace.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), filepath.FromSlash("units/file.nix"))
	return workspace.Parse(path, docstore.NewDocument(fileuri.FromPath(path), "nix", 1, text), nil, nil)
}

func nixEnv() navigation.Env {
	return navigation.Env{Loader: loader{ws: workspace.New(func(string) (*workspace.File, bool) { return nil, false }, workspace.Detect)}}
}

func variableFindings(t *testing.T, text string) []string {
	t.Helper()
	file := nixFile(t, text)
	var out []string
	for _, f := range Check(file, nixEnv(), Settings{}).Findings {
		if f.Code == 2034 || f.Code == 2154 {
			out = append(out, fmt.Sprintf("%v:%s", f.Code, file.Doc.Text()[f.Span.Start:f.Span.End]))
		}
	}
	return out
}

func TestVariablesStayWithinTheirScript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, src string
		want      []string
	}{
		{
			name: "one script assigns and another reads",
			src:  "{ pkgs }: [\n  (pkgs.writeShellScript \"a\" ''\n    shared=1\n  '')\n  (pkgs.writeShellScript \"b\" ''\n    echo \"$shared\"\n  '')\n]\n",
			want: []string{"SC2034:shared=1", "SC2154:$shared"},
		},
		{
			name: "applications are separate programs",
			src:  "{ pkgs }: {\n  a = pkgs.writeShellApplication { name = \"a\"; text = ''\n    shared=1\n  ''; };\n  b = pkgs.writeShellApplication { name = \"b\"; text = ''\n    echo \"$shared\"\n  ''; };\n}\n",
			want: []string{"SC2034:shared=1", "SC2154:$shared"},
		},
		{
			name: "a script alone keeps its own findings",
			src:  "{ pkgs }: pkgs.writeShellScript \"a\" ''\n  shared=1\n  echo \"$other\"\n''\n",
			want: []string{"SC2034:shared=1", "SC2154:$other"},
		},
		{
			name: "phases of one derivation share a shell",
			src:  "{ stdenv }: stdenv.mkDerivation {\n  buildPhase = ''\n    shared=1\n  '';\n  installPhase = ''\n    echo \"$shared\"\n  '';\n}\n",
		},
		{
			name: "phases joined by an update share a shell",
			src:  "{ stdenv }: stdenv.mkDerivation ({\n  buildPhase = ''\n    shared=1\n  '';\n} // {\n  installPhase = ''\n    echo \"$shared\"\n  '';\n})\n",
		},
		{
			name: "phases of two derivations do not",
			src:  "{ stdenv }: [\n  (stdenv.mkDerivation {\n    buildPhase = ''\n      shared=1\n    '';\n  })\n  (stdenv.mkDerivation {\n    installPhase = ''\n      echo \"$shared\"\n    '';\n  })\n]\n",
			want: []string{"SC2034:shared=1"},
		},
		{
			name: "an override runs with the phases it patches",
			src:  "{ pkg, stdenv }: [\n  (stdenv.mkDerivation {\n    installPhase = ''\n      echo \"$shared\"\n    '';\n  })\n  (pkg.overrideAttrs (old: {\n    buildPhase = ''\n      shared=1\n    '';\n  }))\n]\n",
		},
		{
			name: "a spliced string belongs to the script around it",
			src:  "{ pkgs, lib, on }: pkgs.writeShellScript \"a\" ''\n  ${lib.optionalString on ''\n    shared=1\n  ''}\n  echo \"$shared\"\n''\n",
		},
		{
			name: "a marked string may be spliced by name into any script",
			src:  "{ pkgs }: let\n  setup = /* bash */ ''\n    shared=1\n  '';\nin pkgs.writeShellScript \"a\" ''\n  ${setup}\n  echo \"$shared\"\n''\n",
		},
		{
			name: "a sourced surface stays visible to every script",
			src:  "{ pkgs, ... }: {\n  programs.bash.initExtra = ''\n    echo \"$shared\"\n  '';\n  a = pkgs.writeShellScript \"a\" ''\n    shared=1\n  '';\n}\n",
		},
		{
			name: "pieces of one option are one script",
			src:  "{ lib, ... }: {\n  systemd.services.x = lib.mkMerge [\n    { script = lib.mkBefore ''\n      shared=1\n    ''; }\n    { script = lib.mkAfter ''\n      echo \"$shared\"\n    ''; }\n  ];\n}\n",
		},
		{
			name: "two steps of a unit are separate processes",
			src:  "{ ... }: {\n  systemd.services.x = {\n    preStart = ''\n      shared=1\n    '';\n    script = ''\n      echo \"$shared\"\n    '';\n  };\n}\n",
			want: []string{"SC2034:shared=1", "SC2154:$shared"},
		},
		{
			name: "a concatenation is one script",
			src:  "{ pkgs }: pkgs.writeShellScript \"a\" (''\n  shared=1\n'' + ''\n  echo \"$shared\"\n'')\n",
		},
		{
			name: "an embedded text is apart from the file around it",
			src:  "{ pkgs }: [\n  (pkgs.writeText \"justfile\" ''\n    r:\n        shared=1; echo \"$other\"\n  '')\n  (pkgs.writeShellScript \"b\" ''\n    other=1\n    echo \"$shared\"\n  '')\n]\n",
			want: []string{"SC2034:other=1", "SC2034:shared=1", "SC2154:$other", "SC2154:$shared"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := variableFindings(t, tt.src)
			slices.Sort(got)
			if !slices.Equal(got, tt.want) {
				t.Errorf("findings = %q, want %q\nin:\n%s", got, tt.want, tt.src)
			}
		})
	}
}

func TestASyntaxErrorStaysInItsScript(t *testing.T) {
	t.Parallel()

	src := "{ pkgs }: [\n  (pkgs.writeShellScript \"a\" ''\n    if true; then\n  '')\n  (pkgs.writeShellScript \"b\" ''\n    shared=1\n  '')\n]\n"
	if got := strings.Join(variableFindings(t, src), ","); got != "SC2034:shared=1" {
		t.Errorf("findings = %q, want the unused variable in the script that parses", got)
	}
}

const replayed = `{ pkgs, lib, stdenv, ... }: {
  first = pkgs.writeShellScript "first" ''
    value=1
    echo $other
    case "$1" in
      *) echo all ;;
      a) echo a ;;
    esac
  '';
  second = pkgs.writeShellScript "second" ''
    other=''${lib.escapeShellArg "x"}
    echo '${pkgs.hello}/bin/hello' $other
    # shellcheck disable=SC2086
    echo $value
    source ./lib.sh
  '';
  drv = stdenv.mkDerivation {
    buildPhase = ''
      phase=1
    '';
    installPhase = ''
      echo "$phase" "$out"
    '';
  };
  programs.bash.initExtra = ''
    greeting=hello
    echo "$unknown"
  '';
  third = pkgs.writeDash "third" ''
    echo "$greeting" "$value"
    [[ -n $value ]]
  '';
  systemd.services.x = {
    script = ''
      unused=1
    '';
    preStart = "echo $unused";
  };
}
`

var replayedEdits = []string{" ", "x", "\n", "$", "\"", "'", "''", "${", "}", "; ", "if ", "fi\n", "shared=1\n", "echo \"$shared\"\n", "(", ")", "# shellcheck disable=SC2034\n"}

func reportsOf(report lint.Report) string {
	return fmt.Sprintf("%+v", report)
}

func TestRememberedReportsEqualAFullRecompute(t *testing.T) {
	t.Parallel()

	source := rand.New(rand.NewPCG(62, 64))
	memory := NewMemory()
	env := nixEnv()
	text := replayed
	for step := range 600 {
		switch at := source.IntN(len(text) + 1); {
		case step%5 == 4 && at < len(text):
			end := min(at+1+source.IntN(4), len(text))
			text = text[:at] + text[end:]
		default:
			text = text[:at] + replayedEdits[source.IntN(len(replayedEdits))] + text[at:]
		}
		if step%50 == 49 {
			text = replayed
		}
		file := nixFile(t, text)
		got := reportsOf(memory.Check(file, env, Settings{}))
		want := reportsOf(Check(nixFile(t, text), env, Settings{}))
		if got != want {
			t.Fatalf("step %d differs from a full recompute\nremembered: %s\nfull:       %s\ntext:\n%s", step, got, want, text)
		}
	}
}

func TestAnEditElsewhereReusesAScriptsReport(t *testing.T) {
	t.Parallel()

	memory := NewMemory()
	env := nixEnv()
	src := "{ pkgs }: [\n  (pkgs.writeShellScript \"a\" ''\n    one=1\n  '')\n  (pkgs.writeShellScript \"b\" ''\n    two=2\n  '')\n]\n"
	memory.Check(nixFile(t, src), env, Settings{})
	held := maps.Clone(memory.reports)
	if len(held) != 2 {
		t.Fatalf("held %d reports, want one for each script", len(held))
	}
	edited := strings.Replace(src, "one=1", "one=12", 1)
	report := memory.Check(nixFile(t, "# moved\n\n"+edited), env, Settings{})
	if len(report.Findings) != 2 {
		t.Fatalf("findings = %+v, want both variables", report.Findings)
	}
	reused := 0
	for key, remembered := range memory.reports {
		if held[key] == remembered && remembered.lastRun == memory.run {
			reused++
		}
	}
	if reused != 1 {
		t.Errorf("%d reports were reused, want the script that did not change", reused)
	}
	if got := report.Findings[1].Span; got.Start <= report.Findings[0].Span.End {
		t.Errorf("the reused finding at %+v was not placed after the changed script's finding %+v", got, report.Findings[0].Span)
	}
}

func TestAMessageNamingALineIsNotReused(t *testing.T) {
	t.Parallel()

	memory := NewMemory()
	env := nixEnv()
	src := "{ pkgs }: [\n  (pkgs.writeShellScript \"a\" ''\n    case \"$1\" in\n      *) echo all ;;\n      a) echo a ;;\n    esac\n  '')\n  (pkgs.writeShellScript \"b\" ''\n    echo b\n  '')\n]\n"
	memory.Check(nixFile(t, src), env, Settings{})
	moved := "\n\n\n" + src
	got := reportsOf(memory.Check(nixFile(t, moved), env, Settings{}))
	if want := reportsOf(Check(nixFile(t, moved), env, Settings{})); got != want {
		t.Errorf("a remembered message kept its old line\nremembered: %s\nfull:       %s", got, want)
	}
}

func TestSourcedFilesInvalidateARememberedReport(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lib := filepath.Join(dir, "lib.sh")
	if err := os.WriteFile(lib, []byte("#!/bin/bash\nfrom_lib=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ws := workspace.New(func(string) (*workspace.File, bool) { return nil, false }, workspace.Detect)
	generation := &countingLoader{ws: ws}
	env := navigation.Env{Loader: generation, Roots: []string{dir}}
	path := filepath.Join(dir, "file.nix")
	src := "{ pkgs }: [\n  (pkgs.writeShellScript \"a\" ''\n    source ./lib.sh\n    echo \"$from_lib\"\n  '')\n  (pkgs.writeShellScript \"b\" ''\n    echo b\n  '')\n]\n"
	open := func() *workspace.File {
		return workspace.Parse(path, docstore.NewDocument(fileuri.FromPath(path), "nix", 1, src), nil, nil)
	}
	memory := NewMemory()
	if got := codes(memory.Check(open(), env, Settings{}).Findings); slices.Contains(got, 2154) {
		t.Fatalf("the sourced file assigns from_lib: %v", got)
	}
	if err := os.WriteFile(lib, []byte("#!/bin/bash\nsomething_else=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	generation.bump()
	got := reportsOf(memory.Check(open(), env, Settings{}))
	if want := reportsOf(Check(open(), env, Settings{})); got != want {
		t.Errorf("a report outlived the file it sourced\nremembered: %s\nfull:       %s", got, want)
	}
}

type countingLoader struct {
	ws         *workspace.Workspace
	generation atomic.Uint64
}

func (l *countingLoader) Load(path string) (*workspace.File, error) { return l.ws.Load(path) }

func (l *countingLoader) Generation() uint64 { return l.generation.Load() }

func (l *countingLoader) bump() { l.generation.Add(1) }

func TestFalseFindingsAScriptAloneCouldHaveAreGone(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, src string }{
		{"a builder writes its output", "{ pkgs }: pkgs.buildFHSEnv {\n  extraInstallCommands = ''\n    mkdir -p $out/bin\n  '';\n}\n"},
		{"an initrd command writes its output", "{ ... }: {\n  boot.initrd.extraUtilsCommands = ''\n    mkdir -p $out/bin\n  '';\n}\n"},
		{"a hole quotes a shell read into a Nix string", "{ pkgs, url }: pkgs.writeShellScript \"a\" ''\n  shared=1\n  curl \"${url \"$shared\"}\"\n''\n"},
		{"a setup hook is sourced", "{ stdenv, writeShellScript }: stdenv.mkDerivation {\n  nativeBuildInputs = [\n    (writeShellScript \"hook\" ''\n      shared=1\n    '')\n  ];\n}\n"},
		{"overrideCabal patches phases elsewhere", "{ overrideCabal, stdenv, a }: [\n  (overrideCabal (drv: {\n    preCheck = ''\n      shared=1\n    '';\n  }) a)\n  (stdenv.mkDerivation {\n    checkPhase = ''\n      echo \"$shared\"\n    '';\n  })\n]\n"},
		{"stdenv helpers take a name", "{ stdenv }: stdenv.mkDerivation {\n  buildCommand = ''\n    declare -A shared=([x]=y)\n    appendToVar shared more\n    concatStringsSep \";\" shared\n  '';\n}\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			file := nixFile(t, tt.src)
			var found []lint.Finding
			for _, f := range Check(file, nixEnv(), Settings{}).Findings {
				if f.Code == 2034 || f.Code == 2154 || f.Code == 2086 {
					found = append(found, f)
				}
			}
			if len(found) > 0 {
				t.Errorf("findings = %+v in:\n%s", found, tt.src)
			}
		})
	}
}

func treeDump(file *workspace.File) string {
	var sb strings.Builder
	for _, variant := range file.Variants() {
		fmt.Fprintf(&sb, "text %q\nerrors %+v\n", variant.Doc.Text(), variant.Tree.Errors)
		parser.Walk(variant.Tree.File, func(n syntax.Node) bool {
			if n != nil {
				fmt.Fprintf(&sb, "%T %d:%d %d:%d\n", n, parser.Offset(n.Pos()), n.Pos().Line(), parser.Offset(n.End()), n.End().Line())
			}
			return true
		})
		for _, d := range variant.Tree.Definitions {
			fmt.Fprintf(&sb, "def %s %+v %+v\n", d.Name, d.Span, d.NameSpan)
		}
		for _, r := range variant.Tree.References {
			fmt.Fprintf(&sb, "ref %s %+v\n", r.Name, r.Span)
		}
	}
	return sb.String()
}

func TestAReusedParseEqualsAFullOne(t *testing.T) {
	t.Parallel()

	source := rand.New(rand.NewPCG(64, 62))
	text := replayed
	path := filepath.Join(t.TempDir(), "file.nix")
	var previous *workspace.File
	open := func(earlier *workspace.File) *workspace.File {
		return workspace.Parse(path, docstore.NewDocument(fileuri.FromPath(path), "nix", 1, text), earlier, nil)
	}
	for step := range 600 {
		at := source.IntN(len(text) + 1)
		if step%5 == 4 && at < len(text) {
			text = text[:at] + text[min(at+1+source.IntN(4), len(text)):]
		} else {
			text = text[:at] + replayedEdits[source.IntN(len(replayedEdits))] + text[at:]
		}
		if step%50 == 49 {
			text = replayed
		}
		current := open(previous)
		if got, want := treeDump(current), treeDump(open(nil)); got != want {
			t.Fatalf("step %d: a reused parse differs from a full one\nreused:\n%s\nfull:\n%s\ntext:\n%s", step, got, want, text)
		}
		previous = current
	}
}
