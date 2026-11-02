package server

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/lsp"
)

func corpusProject(t *testing.T, name string) map[string]string {
	t.Helper()
	root := filepath.Join("..", "..", "corpus", "nix", name)
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".nix" {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestNixDendriticLayoutThroughTheServer(t *testing.T) {
	t.Parallel()

	const reader = "modules/editor.nix"
	const setter = "modules/core/lib/owner.nix"
	p := openNativeProject(t, nativeCapabilities, corpusProject(t, "dendritic"), reader, setter, "modules/core/paths.nix", "modules/hosts/laptop.nix")

	definition := decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params(reader, "ownerName", 1, "")))
	if want := []lsp.Location{p.at(setter, "ownerName", 0)}; !slices.Equal(definition, want) {
		t.Errorf("definition = %+v, want %+v", definition, want)
	}
	references := decodeResult[[]lsp.Location](t, p.c.call("textDocument/references", p.params(setter, "ownerName", 0, `, "context": {"includeDeclaration": true}`)))
	if want := []lsp.Location{p.at(setter, "ownerName", 0), p.at(reader, "ownerName", 1)}; !slices.Equal(references, want) {
		t.Errorf("references = %+v, want %+v", references, want)
	}
	hover := decodeResult[*lsp.Hover](t, p.c.call("textDocument/hover", p.params(reader, "ownerName", 1, "")))
	if hover == nil || !strings.Contains(hover.Contents.Value, "owner.nix") {
		t.Errorf("hover = %+v", hover)
	}
	highlights := decodeResult[[]lsp.DocumentHighlight](t, p.c.call("textDocument/documentHighlight", p.params(reader, "ownerName", 1, "")))
	if len(highlights) != 1 {
		t.Errorf("highlights in one file = %+v", highlights)
	}
	symbols := decodeResult[[]lsp.SymbolInformation](t, p.c.call("workspace/symbol", `{"query": "stateDir"}`))
	files := map[string]bool{}
	for _, s := range symbols {
		files[s.Location.URI] = true
	}
	for _, name := range []string{"modules/core/paths.nix", "modules/hosts/laptop.nix"} {
		if !files[p.uri(name)] {
			t.Errorf("workspace symbols %+v miss %s", symbols, name)
		}
	}

	const option = "modules/core/paths.nix"
	edit := decodeResult[lsp.WorkspaceEdit](t, p.c.call("textDocument/rename", p.params(option, "stateDir", 0, `, "newName": "stateRoot"`)))
	renamed := map[string]int{}
	for _, change := range edit.DocumentChanges {
		renamed[change.TextDocument.URI] += len(change.Edits)
	}
	for name, want := range map[string]int{option: 2, reader: 1, "modules/hosts/laptop.nix": 1} {
		if renamed[p.uri(name)] != want {
			t.Errorf("rename edits %d places in %s, want %d: %+v", renamed[p.uri(name)], name, want, renamed)
		}
	}
	response := p.c.call("textDocument/rename", p.params(setter, "ownerName", 0, `, "newName": "alpha"`))
	if response.Error == nil || !strings.Contains(response.Error.Message, "passed on") {
		t.Errorf("renaming a name of a set that leaves = %+v", response)
	}
}

func TestNixFlakePartsLayoutThroughTheServer(t *testing.T) {
	t.Parallel()

	const reader = "nix/shell.nix"
	const options = "nix/project.nix"
	p := openNativeProject(t, nativeCapabilities, corpusProject(t, "flake-parts"), reader, "flake.nix")
	definition := decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params(reader, "syncTools", 0, "")))
	if want := []lsp.Location{p.at(options, "syncTools", 0)}; !slices.Equal(definition, want) {
		t.Errorf("definition = %+v, want %+v", definition, want)
	}
	files := decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params("flake.nix", "./nix/checks.nix", 0, "")))
	if len(files) != 1 || files[0].URI != p.uri("nix/checks.nix") {
		t.Errorf("definition of a path = %+v", files)
	}
}

func TestNixSingleFlakeLayoutThroughTheServer(t *testing.T) {
	t.Parallel()

	const caller = "flake.nix"
	const callee = "pkgs/rotate.nix"
	p := openNativeProject(t, nativeCapabilities, corpusProject(t, "single-flake"), caller, callee)
	definition := decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params(caller, "retentionDays", 0, "")))
	if want := []lsp.Location{p.at(callee, "retentionDays", 0)}; !slices.Equal(definition, want) {
		t.Errorf("definition = %+v, want %+v", definition, want)
	}
	edit := decodeResult[lsp.WorkspaceEdit](t, p.c.call("textDocument/rename", p.params(callee, "retentionDays", 0, `, "newName": "keepDays"`)))
	renamed := map[string]int{}
	for _, change := range edit.DocumentChanges {
		renamed[change.TextDocument.URI] += len(change.Edits)
	}
	if renamed[p.uri(caller)] != 1 || renamed[p.uri(callee)] != 2 {
		t.Errorf("rename edits %+v", renamed)
	}
}
