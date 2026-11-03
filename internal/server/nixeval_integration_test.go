//go:build unix

package server

import (
	"encoding/json"
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

const (
	realEvaluationBudget = 2 * time.Minute
	realEvaluationPoll   = 50 * time.Millisecond
	realRequestBudget    = time.Second
)

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
	skipWithoutLockedInputs(t, root, name)
	c, srv := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", "initializationOptions": {"nix": {"evaluate": true}}, `+nativeCapabilities+`}`)
	c.notify("initialized", `{}`)
	return &nixEvalProject{c: c, srv: srv, root: root}
}

func skipWithoutLockedInputs(t *testing.T, root, name string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "nix", "--extra-experimental-features", "nix-command flakes", "flake", "archive", "--no-write-lock-file", "path:"+root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("the inputs corpus flake %s locks are not in the local store and cannot be fetched: %v\n%s", name, err, out)
	}
}

func awaitRealEvaluation[T any](t *testing.T, p *nixEvalProject, what string, ask func() (T, bool)) T {
	t.Helper()
	started := time.Now()
	for {
		asked := time.Now()
		value, ready := ask()
		if took := time.Since(asked); took > realRequestBudget {
			t.Errorf("%s: one request took %s, and a request must not wait for evaluation", what, took.Round(time.Millisecond))
		}
		if ready {
			t.Logf("%s answered after %s", what, time.Since(started).Round(time.Millisecond))
			return value
		}
		if time.Since(started) > realEvaluationBudget {
			t.Fatalf("%s did not settle within %s: last answer %+v, notices %s", what, realEvaluationBudget, value, shownNotices(p))
		}
		time.Sleep(realEvaluationPoll)
	}
}

func shownNotices(p *nixEvalProject) string {
	var out []string
	for _, m := range messagesOf(p.c, showMessageMethod) {
		var shown lsp.ShowMessageParams
		if err := json.Unmarshal(m.Params, &shown); err != nil {
			out = append(out, string(m.Params))
			continue
		}
		out = append(out, shown.Message)
	}
	return strings.Join(out, "; ")
}

func awaitNixLabel(t *testing.T, p *nixEvalProject, params, want string) {
	t.Helper()
	labels := awaitRealEvaluation(t, p, "completion of "+want, func() ([]string, bool) {
		list, labels := p.labels(t, params)
		return labels, !list.IsIncomplete
	})
	if !slices.Contains(labels, want) {
		t.Fatalf("the settled completion lacks %s: %q, notices %s", want, labels, shownNotices(p))
	}
}

func awaitNarrowedNixLabel(t *testing.T, p *nixEvalProject, params, want string) {
	t.Helper()
	list := awaitRealEvaluation(t, p, "narrowed completion of "+want, func() (lsp.CompletionList, bool) {
		list, labels := p.labels(t, params)
		return list, slices.Contains(labels, want)
	})
	if !list.IsIncomplete {
		t.Errorf("a completion narrowed by what is typed is not asked for again: %+v", list)
	}
}

func TestRealFlakePartsCompletionAndHover(t *testing.T) {
	p := openRealCorpusFlake(t, "flake-parts")
	awaitNixLabel(t, p, p.open(t, "nix/fresh.nix", "{ perSystem = { ... }: { | }; }"), "packages")
	awaitNixLabel(t, p, p.open(t, "nix/fresh2.nix", "{ perSystem = { ... }: { project.| }; }"), "syncTools")
	awaitNixLabel(t, p, p.open(t, "nix/top.nix", "{ project.| }"), "version")
	params := p.open(t, "nix/hover.nix", "{ perSystem = { ... }: { project.sync|Tools = [ ]; }; }")
	hover := awaitRealEvaluation(t, p, "hover of syncTools", func() (*lsp.Hover, bool) {
		hover := decodeResult[*lsp.Hover](t, p.c.call("textDocument/hover", params))
		return hover, hover != nil
	})
	if !strings.Contains(hover.Contents.Value, "Programs the sync script needs on PATH.") || !strings.Contains(hover.Contents.Value, "nix/project.nix") {
		t.Errorf("hover = %+v", hover)
	}
}

func TestRealSingleFlakeNixpkgsCompletion(t *testing.T) {
	p := openRealCorpusFlake(t, "single-flake")
	awaitNarrowedNixLabel(t, p, p.open(t, "x.nix", "{ lib, ... }: lib.mk|"), "mkIf")
}
