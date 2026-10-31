package nix

import (
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/projection"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

const phase = "# shellcheck disable=SC2154\nnixPhase%d() {\nset -o errexit -o nounset -o pipefail\n"

func phaseOpen(n int) string {
	return strings.Replace(phase, "%d", string(rune('0'+n)), 1)
}

func phaseClose(n int) string {
	return "\n:\n}\nnixPhase" + string(rune('0'+n)) + "\n"
}

func TestProjectText(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, nix, want string
	}{
		{
			name: "a phase loses its indentation and runs as a stdenv function",
			nix:  "stdenv.mkDerivation {\n  pname = \"x\";\n  installPhase = ''\n    mkdir -p $out\n      cp x $out\n  '';\n}\n",
			want: phaseOpen(0) + "mkdir -p $out\n  cp x $out\n" + phaseClose(0),
		},
		{
			name: "escapes and antiquotations",
			nix:  "{ buildPhase = ''\n  make ${toString jobs} '''a''' ''${b} ''\\n '${c}'\n''; }",
			want: phaseOpen(0) + "make $$ ''a'' ${b} \n ''$$''\n" + phaseClose(0),
		},
		{
			name: "joined strings are one script",
			nix:  "{ postInstall = ''\n  if true; then\n'' + lib.optionalString stdenv.isLinux ''\n  echo linux\n'' + ''\n  fi\n''; }",
			want: phaseOpen(0) + "if true; then\n\necho linux\n\nfi\n" + phaseClose(0),
		},
		{
			name: "a default after or",
			nix:  "{ installPhase = attrs.installPhase or (''\n  make install\n''); }",
			want: phaseOpen(0) + "make install\n" + phaseClose(0),
		},
		{
			name: "double-quoted strings",
			nix:  "{ preBuild = \"export A=\\\"${a}\\\" B=\\${b}\\n\"; }",
			want: phaseOpen(0) + "export A=\"$$\" B=${b}\n" + phaseClose(0),
		},
		{
			name: "writers and runCommand",
			nix:  "[ (writeShellScript \"a\" ''\n  echo a\n'') (writers.writeDash \"b\" ''\n  echo b\n'') (runCommand \"c\" { } ''\n  echo c > $out\n'') ]",
			want: "(\necho a\n\n:\n)\n(\necho b\n\n:\n)\n" + phaseOpen(2) + "echo c > $out\n" + phaseClose(2),
		},
		{
			name: "writeScript runs what its shebang names",
			nix:  "[ (writeScript \"a\" ''\n  #!/bin/sh\n  echo a\n'') (writeScript \"b\" ''\n  #!${python3}/bin/python\n  print(1)\n'') ]",
			want: "(\necho a\n\n:\n)\n",
		},
		{
			name: "writeShellApplication is strict and honours its exclusions",
			nix:  "writeShellApplication {\n  name = \"x\";\n  text = ''\n    echo $1\n  '';\n  excludeShellChecks = [ \"SC2086\" ];\n}",
			want: "# shellcheck disable=SC2086\n(\nset -o errexit -o nounset -o pipefail\necho $1\n\n:\n)\n",
		},
		{
			name: "systemd scripts and shell hooks are scripts of their own",
			nix:  "{ systemd.services.x.script = ''\n  echo x\n''; shell = mkShell { shellHook = ''\n  echo hook\n''; }; }",
			want: "(\necho x\n\n:\n)\n(\necho hook\n\n:\n)\n",
		},
		{
			name: "markers opt a string in",
			nix:  "{ cmd = /* bash */ ''\n  echo marked\n''; }",
			want: "(\necho marked\n\n:\n)\n",
		},
		{
			name: "other strings are not shell",
			nix:  "{ description = \"echo $x\"; script = ''\n  print(1)\n''; text = ''\n  echo\n''; extraConfig = ''\n  rm -rf /\n''; buildInputs = [ \"echo\" ]; src = ~/x; e = <nixpkgs>; }",
			want: "",
		},
		{
			name: "comments, paths and nested strings do not end strings early",
			nix:  "{ a = ./x; /* '' */ # ''\n  buildPhase = ''\n  echo ${lib.concatMapStrings (x: ''${x} '') [ \"}\" ]}\n''; }",
			want: phaseOpen(0) + "echo $$\n" + phaseClose(0),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := Analyze(tt.nix).Map
			if m.Text() != tt.want {
				t.Errorf("Analyze(%q) =\n%q\nwant\n%q", tt.nix, m.Text(), tt.want)
			}
		})
	}
}

func TestProjectDialect(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, nix string
		want      dialect.Detection
	}{
		{"nothing", "{ }", dialect.Detection{Dialect: dialect.Posix, Source: dialect.SourceHostDefault, Evidence: "nix"}},
		{"stdenv", "{ buildPhase = ''make''; }", dialect.Detection{Dialect: dialect.Bash, Source: dialect.SourceHostDefault, Evidence: "nix"}},
		{"dash writers", "writeDash \"x\" ''echo''", dialect.Detection{Dialect: dialect.Dash, Source: dialect.SourceHostSetting, Evidence: "writeDash"}},
		{"a marker", "{ x = /* zsh */ ''print''; }", dialect.Detection{Dialect: dialect.Zsh, Source: dialect.SourceHostSetting, Evidence: "/* zsh */"}},
		{"a shebang", "writeScript \"x\" ''\n  #!/usr/bin/env ksh\n  print\n''", dialect.Detection{Dialect: dialect.Ksh, Source: dialect.SourceHostSetting, Evidence: "#!/usr/bin/env ksh"}},
	} {
		if got := Analyze(tt.nix).Detection; got != tt.want {
			t.Errorf("%s: detection = %+v, want %+v", tt.name, got, tt.want)
		}
	}
}

func TestProjectEdits(t *testing.T) {
	t.Parallel()

	indented := "{ buildPhase = ''\n    echo `date` ${a}\n  ''; }"
	quoted := "{ buildPhase = \"echo `date` ${a}\"; }"
	for _, tt := range []struct {
		name, src, old, text, want string
		ok                         bool
	}{
		{"an indented string", indented, "`date`", "\"${x}\" ''", "{ buildPhase = ''\n    echo \"''${x}\" ''' ${a}\n  ''; }", true},
		{"new lines keep the indentation", indented, "`date`", "a\nb", "{ buildPhase = ''\n    echo a\n    b ${a}\n  ''; }", true},
		{"a quote next to a quote", "{ buildPhase = ''\n  echo '\n''; }", "echo ", "echo '", "", false},
		{"a dollar before a brace", "{ buildPhase = ''\n  echo {a}\n''; }", "echo ", "echo $", "", false},
		{"a double-quoted string", quoted, "`date`", "\"$(date)\"\\", "{ buildPhase = \"echo \\\"$(date)\\\"\\\\ ${a}\"; }", true},
	} {
		m := Analyze(tt.src).Map
		start := strings.Index(m.Text(), tt.old)
		if start < 0 {
			t.Fatalf("%s: %q not in %q", tt.name, tt.old, m.Text())
		}
		got, ok := m.Edit(textedit.Edit{Start: start, End: start + len(tt.old), NewText: tt.text})
		if ok != tt.ok {
			t.Errorf("%s: ok = %v, want %v", tt.name, ok, tt.ok)
			continue
		}
		if applied := textedit.Apply(tt.src, []textedit.Edit{got}); ok && applied != tt.want {
			t.Errorf("%s: applying %+v gives %q, want %q", tt.name, got, applied, tt.want)
		}
	}
}

func TestProjectPositions(t *testing.T) {
	t.Parallel()

	src := "{ buildPhase = ''\n    cp ${src} $out\n  ''; }"
	m := Analyze(src).Map
	for _, tt := range []struct {
		proj, host string
		inside     bool
	}{{"cp", "cp", true}, {"$$", "${src}", false}, {"$out", "$out", true}} {
		start, hostStart := projection.ProjOffset(strings.Index(m.Text(), tt.proj)), projection.HostOffset(strings.Index(src, tt.host))
		if got := m.HostOffset(start); got != hostStart {
			t.Errorf("HostOffset of %q = %d, want %d", tt.proj, got, hostStart)
		}
		if got, inside := m.Offset(hostStart); got != start || inside != tt.inside {
			t.Errorf("Offset of %q = %d, %v, want %d, %v", tt.host, got, inside, start, tt.inside)
		}
	}
}

func TestAnalyzeExportsTheEnvironmentOfShellStrings(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, nix string
		want      []string
	}{
		{
			name: "derivation attributes, inherited ones and env",
			nix:  "stdenv.mkDerivation (finalAttrs: {\n  pname = \"x\";\n  inherit version;\n  inherit (src) rev owner;\n  \"QUOTED\" = 1;\n  \"not-a-name\" = 1;\n  env.CFLAGS = \"-O2\";\n  env = { LDFLAGS = \"\"; };\n  meta.description = \"\";\n  passthru = { tests = 1; };\n  __structuredAttrs = false;\n  installPhase = ''\n    echo $pname\n  '';\n})",
			want: []string{"pname", "version", "rev", "owner", "QUOTED", "CFLAGS", "LDFLAGS", "installPhase"},
		},
		{
			name: "let bindings, with and assert inside values are not attributes",
			nix:  "{ a = let b = 1; c = let d = 2; in d; in b; e = with lib; f; g = assert true; h; buildPhase = ''\n  make\n''; }",
			want: []string{"a", "e", "g", "buildPhase"},
		},
		{
			name: "runCommand exports its attribute set",
			nix:  "runCommand \"x\" { FOO = \"1\"; meta = { }; } ''\n  echo $FOO > $out\n''",
			want: []string{"FOO"},
		},
		{
			name: "a systemd unit exports its environment",
			nix:  "{ systemd.services.x = { environment.A = \"1\"; environment = { B = \"2\"; }; path = [ ]; script = ''\n  echo $A $B\n''; }; }",
			want: []string{"A", "B"},
		},
		{
			name: "writeShellApplication exports its runtimeEnv",
			nix:  "writeShellApplication { name = \"x\"; runtimeEnv = { A = \"1\"; }; text = ''\n  echo $A\n''; }",
			want: []string{"A"},
		},
		{
			name: "writers and marked strings have no environment",
			nix:  "{ a = 1; b = writeShellScript \"x\" ''\n  echo\n''; c = /* bash */ ''\n  echo\n''; }",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := Analyze(tt.nix)
			var got []string
			for _, s := range a.Index.Symbols {
				got = append(got, s.Name)
				if !s.Exported || s.Scope == 0 || tt.nix[s.Selection.Start:s.Selection.End] != s.Name {
					t.Errorf("symbol %+v is not an exported variable named where it is defined", s)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("exported %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAnalyzeShellsLeaveOutAntiquotations(t *testing.T) {
	t.Parallel()

	src := "{ pname = \"p\"; buildPhase = ''\n  cp ${src} $out\n''; installPhase = \"echo ${a}$pname\"; }"
	a := Analyze(src)
	var got []string
	for _, s := range a.Index.Shells {
		got = append(got, src[s.Span.Start:s.Span.End])
		if s.Scope != a.Index.Shells[0].Scope {
			t.Errorf("the phases of one derivation are in different scopes: %+v", a.Index.Shells)
		}
	}
	if want := []string{"\n  cp ", " $out\n", "echo ", "$pname"}; !slices.Equal(got, want) {
		t.Errorf("shells = %q, want %q", got, want)
	}
	if got := a.Index.Symbols[0].Detail; got != `pname = "p";` {
		t.Errorf("detail = %q", got)
	}
	inner := strings.Index(src, "$out")
	if spans := a.Enclosing(inner); len(spans) != 2 || src[spans[1].Start:spans[1].End] != "''\n  cp ${src} $out\n''" {
		t.Errorf("Enclosing(%d) = %+v", inner, spans)
	}
}
