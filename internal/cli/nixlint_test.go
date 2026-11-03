package cli

import (
	"slices"
	"strings"
	"testing"
)

func codesIn(out string) []string {
	var codes []string
	for line := range strings.Lines(out) {
		if m := gccLine.FindStringSubmatch(strings.TrimRight(line, "\n")); m != nil {
			codes = append(codes, m[4])
		}
	}
	slices.Sort(codes)
	return codes
}

func TestNixChecksHonourStatixConfiguration(t *testing.T) {
	t.Parallel()

	const source = "let in x: { a = https://example.org; b = x; }\n"
	for _, tt := range []struct {
		name   string
		config string
		file   string
		want   []string
	}{
		{"no configuration", "", "a.nix", []string{"SL8013", "SL8022"}},
		{"a disabled lint", "disabled = [\"unquoted_uri\"]\n", "a.nix", []string{"SL8013"}},
		{"from a directory above", "disabled = [\"empty_let_in\"]\n", "deep/er/a.nix", []string{"SL8022"}},
		{"an unknown name", "disabled = [\"nope\"]\n", "a.nix", []string{"SL8013", "SL8022"}},
		{"an ignored file", "ignore = [\"gen\"]\n", "gen/a.nix", nil},
		{"another file", "ignore = [\"gen\"]\n", "a.nix", []string{"SL8013", "SL8022"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			files := map[string]string{tt.file: source}
			if tt.config != "" {
				files["statix.toml"] = tt.config
			}
			writeFiles(t, dir, files)
			r := run(t, dir, "", "check", "-f", "gcc", tt.file)
			if got := codesIn(r.stdout); !slices.Equal(got, tt.want) {
				t.Errorf("codes = %q, want %q (stdout %q, stderr %q)", got, tt.want, r.stdout, r.stderr)
			}
		})
	}
}

func TestBrokenStatixConfigurationIsAnError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"statix.toml": "not toml\n", "a.nix": "1\n"})
	r := run(t, dir, "", "check", "a.nix")
	if r.status != checkStatusOptions || !strings.Contains(r.stdout+r.stderr, "statix.toml") {
		t.Errorf("status %d, stdout %q, stderr %q", r.status, r.stdout, r.stderr)
	}
}

func TestOptionalNixChecksAreEnabledByName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"a.nix": "{ a.a = 1; a.b = 2; a.c = 3; }\n"})
	if got := codesIn(run(t, dir, "", "check", "-f", "gcc", "a.nix").stdout); len(got) != 0 {
		t.Errorf("by default: %q", got)
	}
	if got := codesIn(run(t, dir, "", "check", "-f", "gcc", "--enable=repeated-keys", "a.nix").stdout); !slices.Equal(got, []string{"SL8026"}) {
		t.Errorf("with the check on: %q", got)
	}
	writeFiles(t, dir, map[string]string{"statix.toml": "disabled = [\"repeated_keys\"]\n"})
	if got := codesIn(run(t, dir, "", "check", "-f", "gcc", "--enable=repeated-keys", "a.nix").stdout); len(got) != 0 {
		t.Errorf("disabled in statix.toml: %q", got)
	}
}

func TestFlakeLockChecksReadTheLockFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"flake.nix":  "{\n  inputs.a.inputs.gone.follows = \"nixpkgs\";\n  inputs.nixpkgs.url = \"x\";\n  outputs = { self, a, nixpkgs }: { inherit a; };\n}\n",
		"flake.lock": `{"root": "root", "version": 7, "nodes": {"root": {"inputs": {"a": "a", "nixpkgs": "nixpkgs"}}, "a": {"inputs": {"nixpkgs": "other"}}, "nixpkgs": {}, "other": {}}}`,
	})
	if got := codesIn(run(t, dir, "", "check", "-f", "gcc", "flake.nix").stdout); !slices.Equal(got, []string{"SL8039"}) {
		t.Errorf("default: %q", got)
	}
	if got := codesIn(run(t, dir, "", "check", "-f", "gcc", "--enable=missing-nixpkgs-follows", "flake.nix").stdout); !slices.Equal(got, []string{"SL8039", "SL8040"}) {
		t.Errorf("with the optional check on: %q", got)
	}
}

func TestCheckLintsAStringProvenThroughAWrapperInAnotherFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"writer.nix": "{ writeShellScript }:\ntext: writeShellScript \"x\" text\n",
		"caller.nix": "{ callPackage }:\nlet\n  w = callPackage ./writer.nix { };\nin\nw ''\n  ls $1\n''\n",
	})
	if got := codesIn(run(t, dir, "", "check", "-f", "gcc", "caller.nix").stdout); !slices.Contains(got, "SC2086") {
		t.Errorf("findings = %q, want the one of the proven string", got)
	}
}
