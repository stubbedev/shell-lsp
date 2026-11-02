package server

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func nixLintCodes(t *testing.T, p *nativeProject, file string) []string {
	t.Helper()
	var got []string
	for _, d := range pullDiagnostics(t, p.c, p.uri(file)) {
		got = append(got, fmt.Sprintf("%d %s", d.Range.Start.Line+1, d.Code))
	}
	slices.Sort(got)
	return got
}

func TestNixChecksFollowStatixConfiguration(t *testing.T) {
	t.Parallel()

	p := openNativeProject(t, nativeCapabilities, map[string]string{
		"statix.toml": "disabled = [\"unquoted_uri\"]\n",
		"default.nix": "let in { a = https://example.org; b = 1; }\n",
	}, "default.nix")
	if got, want := nixLintCodes(t, p, "default.nix"), []string{"1 SL8013"}; !slices.Equal(got, want) {
		t.Errorf("diagnostics = %q, want %q", got, want)
	}
}

func TestNixDiagnosticsFollowAnEditedStatixConfiguration(t *testing.T) {
	t.Parallel()

	p := openNativeProject(t, nativeCapabilities, map[string]string{
		"statix.toml": "disabled = [\"unquoted_uri\"]\n",
		"default.nix": "{ a = https://example.org; }\n",
	}, "default.nix")
	if got := nixLintCodes(t, p, "default.nix"); len(got) != 0 {
		t.Fatalf("diagnostics = %q", got)
	}
	if err := os.WriteFile(filepath.Join(p.root, "statix.toml"), []byte("disabled = []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p.c.notify("workspace/didChangeWatchedFiles", fmt.Sprintf(`{"changes": [{"uri": %q, "type": 2}]}`, p.uri("statix.toml")))
	if got, want := nixLintCodes(t, p, "default.nix"), []string{"1 SL8022"}; !slices.Equal(got, want) {
		t.Errorf("diagnostics = %q, want %q", got, want)
	}
}
