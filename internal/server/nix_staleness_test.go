package server

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

const (
	stalenessReader = "modules/editor.nix"
	stalenessSetter = "modules/core/lib/owner.nix"
)

func (p *nativeProject) replace(name, text string, version int) {
	p.t.Helper()
	p.files[name] = text
	p.c.notify("textDocument/didChange", fmt.Sprintf(`{"textDocument": {"uri": %q, "version": %d}, "contentChanges": [{"text": %q}]}`, p.uri(name), version, text))
}

func TestNixDefinitionFollowsAnEditInAnotherFile(t *testing.T) {
	t.Parallel()

	p := openNativeProject(t, nativeCapabilities, corpusProject(t, "dendritic"), stalenessReader, stalenessSetter)
	definition := decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params(stalenessReader, "ownerName", 1, "")))
	if want := []lsp.Location{p.at(stalenessSetter, "ownerName", 0)}; !slices.Equal(definition, want) {
		t.Fatalf("before the edit, definition = %+v, want %+v", definition, want)
	}
	p.replace(stalenessSetter, "\n\n"+p.files[stalenessSetter], 2)
	definition = decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params(stalenessReader, "ownerName", 1, "")))
	if want := []lsp.Location{p.at(stalenessSetter, "ownerName", 0)}; !slices.Equal(definition, want) {
		t.Errorf("after the edit, definition = %+v, want %+v", definition, want)
	}
}

func TestNixReferencesFollowARemovedRead(t *testing.T) {
	t.Parallel()

	p := openNativeProject(t, nativeCapabilities, corpusProject(t, "dendritic"), stalenessReader, stalenessSetter)
	const declared = `, "context": {"includeDeclaration": true}`
	references := decodeResult[[]lsp.Location](t, p.c.call("textDocument/references", p.params(stalenessSetter, "ownerName", 0, declared)))
	if want := []lsp.Location{p.at(stalenessSetter, "ownerName", 0), p.at(stalenessReader, "ownerName", 1)}; !slices.Equal(references, want) {
		t.Fatalf("before the edit, references = %+v, want %+v", references, want)
	}
	p.replace(stalenessReader, strings.ReplaceAll(p.files[stalenessReader], "ownerName", "somethingElse"), 2)
	references = decodeResult[[]lsp.Location](t, p.c.call("textDocument/references", p.params(stalenessSetter, "ownerName", 0, declared)))
	if want := []lsp.Location{p.at(stalenessSetter, "ownerName", 0)}; !slices.Equal(references, want) {
		t.Errorf("after the read is gone, references = %+v, want %+v", references, want)
	}
}

func TestNixDefinitionFollowsARemovedBinding(t *testing.T) {
	t.Parallel()

	p := openNativeProject(t, nativeCapabilities, corpusProject(t, "dendritic"), stalenessReader, stalenessSetter)
	p.replace(stalenessSetter, strings.ReplaceAll(p.files[stalenessSetter], "ownerName", "somethingElse"), 2)
	definition := decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params(stalenessReader, "ownerName", 1, "")))
	if slices.ContainsFunc(definition, func(l lsp.Location) bool { return l.URI == p.uri(stalenessSetter) }) {
		t.Errorf("definition = %+v still names the removed binding", definition)
	}
}

func addedModule(name string) string {
	return "{ config, ... }:\n{\n  config.harbor.lib." + name + " = config.harbor.lib.ownerName;\n}\n"
}

func TestNixReferencesSeeANewFileUnderImportTreeOnANotification(t *testing.T) {
	t.Parallel()

	const added = "modules/added.nix"
	p := openNativeProject(t, nativeCapabilities, corpusProject(t, "dendritic"), stalenessReader, stalenessSetter)
	const declared = `, "context": {"includeDeclaration": true}`
	before := decodeResult[[]lsp.Location](t, p.c.call("textDocument/references", p.params(stalenessSetter, "ownerName", 0, declared)))
	path := filepath.Join(p.root, added)
	if err := os.WriteFile(path, []byte(addedModule("added")), 0o600); err != nil {
		t.Fatal(err)
	}
	p.c.notify("workspace/didChangeWatchedFiles", fmt.Sprintf(`{"changes": [{"uri": %q, "type": 1}]}`, fileuri.FromPath(path)))
	references := decodeResult[[]lsp.Location](t, p.c.call("textDocument/references", p.params(stalenessSetter, "ownerName", 0, declared)))
	if len(references) != len(before)+1 || !slices.ContainsFunc(references, func(l lsp.Location) bool { return l.URI == fileuri.FromPath(path) }) {
		t.Errorf("references = %+v, want the %d from before and one in %s", references, len(before), added)
	}
}

func TestNixDefinitionSeesADiskEditWithoutANotification(t *testing.T) {
	t.Parallel()

	p := openNativeProject(t, nativeCapabilities, corpusProject(t, "dendritic"), stalenessReader)
	shifted := "\n\n" + p.files[stalenessSetter]
	if err := os.WriteFile(filepath.Join(p.root, stalenessSetter), []byte(shifted), 0o600); err != nil {
		t.Fatal(err)
	}
	p.files[stalenessSetter] = shifted
	want := p.at(stalenessSetter, "ownerName", 0)
	deadline := time.Now().Add(clientTimeout)
	for {
		definition := decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params(stalenessReader, "ownerName", 1, "")))
		if slices.Contains(definition, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("definition = %+v, want %+v", definition, want)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestNixRequestDuringALinkAnswersFromTheOpenFile(t *testing.T) {
	t.Parallel()

	files := corpusProject(t, "dendritic")
	const local = "modules/local.nix"
	files[local] = "let\n  shown = 1;\nin\n{ value = shown; }\n"
	p := openNativeProjectWith(t, func(s *Server) { s.linkPatience = time.Nanosecond; s.nixEval.strings.quiet = time.Hour }, nativeCapabilities, files, local)
	definition := decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params(local, "shown", 1, "")))
	if want := []lsp.Location{p.at(local, "shown", 0)}; !slices.Equal(definition, want) {
		t.Errorf("definition of a local binding = %+v, want %+v", definition, want)
	}
}
