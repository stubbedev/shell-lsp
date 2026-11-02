//go:build unix

package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

const realEvaluationPatience = 5 * time.Minute

func openRealCorpusFlake(t *testing.T, name string) *nixEvalProject {
	t.Helper()
	if testing.Short() {
		t.Skip("evaluating a corpus flake can fetch its inputs")
	}
	if _, err := exec.LookPath("nix"); err != nil {
		t.Skip("nix is not installed")
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join("..", "..", "corpus", "nix", name))); err != nil {
		t.Fatal(err)
	}
	c, srv := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", "initializationOptions": {"nix": {"evaluate": true}}, `+nativeCapabilities+`}`)
	c.notify("initialized", `{}`)
	return &nixEvalProject{c: c, srv: srv, root: root}
}

func awaitNixLabel(t *testing.T, p *nixEvalProject, params, want string) lsp.CompletionList {
	t.Helper()
	deadline := time.Now().Add(realEvaluationPatience)
	for {
		list, labels := p.labels(t, params)
		if slices.Contains(labels, want) && !list.IsIncomplete {
			return list
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not appear: %q, incomplete %t", want, labels, list.IsIncomplete)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func TestRealFlakePartsCompletionAndHover(t *testing.T) {
	p := openRealCorpusFlake(t, "flake-parts")
	awaitNixLabel(t, p, p.open(t, "nix/fresh.nix", "{ perSystem = { ... }: { | }; }"), "packages")
	awaitNixLabel(t, p, p.open(t, "nix/fresh2.nix", "{ perSystem = { ... }: { project.| }; }"), "syncTools")
	awaitNixLabel(t, p, p.open(t, "nix/top.nix", "{ project.| }"), "version")
	hover := decodeResult[*lsp.Hover](t, p.c.call("textDocument/hover", p.open(t, "nix/hover.nix", "{ perSystem = { ... }: { project.sync|Tools = [ ]; }; }")))
	if hover == nil || !strings.Contains(hover.Contents.Value, "Programs the sync script needs on PATH.") || !strings.Contains(hover.Contents.Value, "nix/project.nix") {
		t.Errorf("hover = %+v", hover)
	}
}

func TestRealSingleFlakeNixpkgsCompletion(t *testing.T) {
	p := openRealCorpusFlake(t, "single-flake")
	awaitNixLabel(t, p, p.open(t, "x.nix", "{ lib, ... }: lib.mk|"), "mkIf")
}
