package hostnav

import (
	"slices"
	"strings"
	"testing"
)

const wrapperPhase = "{ stdenv, makeWrapper }:\nstdenv.mkDerivation {\n  nativeBuildInputs = [ makeWrapper ];\n  postInstall = ''\n    wrapProgram $out/bin/x --set FOO 1\n    substituteInPlace a.txt --replace-fail a b\n  '';\n}\n"

func TestBuilderCommandsAreKnownInAPhase(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, source, sub, want string
	}{
		{"a setup hook of an input", wrapperPhase, "wrapProgram", "setup hook of `makeWrapper`"},
		{"a function of setup.sh", wrapperPhase, "substituteInPlace", "substituteInPlace FILE ARGS"},
		{"a wrapper flag", wrapperPhase, "--set", "--set VAR VAL"},
		{"a substitution flag", wrapperPhase, "--replace-fail", "--replace-fail OLD NEW"},
		{"without the input it is still a builder command", "{ stdenv }:\nstdenv.mkDerivation {\n  postInstall = ''\n    wrapProgram $out/bin/x\n  '';\n}\n", "wrapProgram", "wrapProgram EXECUTABLE ARGS"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tr := newTree(t, map[string]string{"default.nix": tt.source})
			g, sym := tr.shellAt("default.nix", tt.sub, 0)
			if got := g.Describe(sym).Markdown; !strings.Contains(got, tt.want) {
				t.Errorf("hover on %s = %q, want it to mention %q", tt.sub, got, tt.want)
			}
		})
	}
}

func TestAHookCommandGoesToTheInputThatBringsIt(t *testing.T) {
	t.Parallel()

	tr := newTree(t, map[string]string{"default.nix": wrapperPhase})
	if got := tr.shellDefinition("default.nix", "wrapProgram"); !slices.Equal(got, []string{"default.nix:1:makeWrapper"}) {
		t.Errorf("definition = %q", got)
	}
}

func TestBuilderCommandsStayInPhases(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, source string
	}{
		{"a script that runs later", "{ writeShellScript }:\nwriteShellScript \"x\" ''\n  wrapProgram a --set b c\n''\n"},
		{"a function of the same name", "{ stdenv }:\nstdenv.mkDerivation {\n  postInstall = ''\n    wrapProgram() { :; }\n    wrapProgram a --set b c\n  '';\n}\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tr := newTree(t, map[string]string{"default.nix": tt.source})
			g := tr.group("default.nix")
			at := strings.LastIndex(tt.source, "--set")
			inner, inside := g.Self.Doc.Positions(0).Offset(g.Self.Doc.Source().OffsetToPosition(at, 0))
			if !inside {
				t.Fatalf("%q is not in the shell", "--set")
			}
			if sym, ok := g.ShellSymbol(inner); ok {
				t.Errorf("--set resolved to %+v", sym)
			}
			if items := BuilderCompletions(g.Self, inner); items != nil {
				t.Errorf("completion = %+v", items)
			}
		})
	}
}

func TestBuilderCompletionsAndSignature(t *testing.T) {
	t.Parallel()

	source := "{ stdenv }:\nstdenv.mkDerivation {\n  postInstall = ''\n    wrapPr\n    wrapProgram $out/bin/x --pre\n    substituteInPlace file \n  '';\n}\n"
	tr := newTree(t, map[string]string{"default.nix": source})
	g := tr.group("default.nix")
	inner := func(needle string, delta int) int {
		t.Helper()
		at := strings.Index(source, needle) + delta
		offset, inside := g.Self.Doc.Positions(0).Offset(g.Self.Doc.Source().OffsetToPosition(at, 0))
		if !inside {
			t.Fatalf("%q is not in the shell", needle)
		}
		return offset
	}
	labels := func(items []Item) []string {
		out := make([]string, len(items))
		for i, it := range items {
			out[i] = it.Label
		}
		return out
	}
	if got := labels(BuilderCompletions(g.Self, inner("wrapPr\n", 6))); !slices.Equal(got, []string{"wrapProgram"}) {
		t.Errorf("command completion = %q", got)
	}
	if got := labels(BuilderCompletions(g.Self, inner("--pre", 5))); !slices.Contains(got, "--prefix") || slices.Contains(got, "--set") {
		t.Errorf("flag completion = %q", got)
	}
	sig, ok := g.ShellSignature(inner("substituteInPlace file ", 23))
	if !ok || sig.Label != "substituteInPlace FILE ARGS" || sig.Active != 1 {
		t.Errorf("signature = %+v, %v", sig, ok)
	}
}
