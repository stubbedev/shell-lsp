package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()

	for name, text := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestConfigurationPullPerFolder(t *testing.T) {
	t.Parallel()

	a, b := t.TempDir(), t.TempDir()
	c, _ := startClient(t, `{"workspaceFolders": [{"uri": "`+fileuri.FromPath(a)+`", "name": "a"}, {"uri": "`+fileuri.FromPath(b)+`", "name": "b"}],
		"capabilities": {"textDocument": {"diagnostic": {}}, "workspace": {"configuration": true, "didChangeWatchedFiles": {"dynamicRegistration": true}}}}`)
	c.notify("initialized", `{}`)

	registered := c.answerServerRequest("client/registerCapability")
	if !strings.Contains(string(registered), `"workspace/didChangeWatchedFiles"`) || !strings.Contains(string(registered), `**/*.{sh,bash,zsh,ksh,dash,bats}`) {
		t.Errorf("registration = %s", registered)
	}

	request := c.await("workspace/configuration", func(m *jsonrpc.Message) bool { return m.IsRequest() && m.Method == "workspace/configuration" })
	var params configurationParams
	if err := json.Unmarshal(request.Params, &params); err != nil {
		t.Fatal(err)
	}
	if len(params.Items) != 3 || params.Items[1].ScopeURI != fileuri.FromPath(a) {
		t.Fatalf("configuration items = %+v", params.Items)
	}
	if err := c.conn.Reply(request.ID, []any{nil, map[string]any{"dialect": "zsh"}, nil}, nil); err != nil {
		t.Fatal(err)
	}

	uriA := fileuri.FromPath(filepath.Join(a, "x.sh"))
	uriB := fileuri.FromPath(filepath.Join(b, "x.sh"))
	c.notify("textDocument/didOpen", openParams(uriA, "print -l *(.)\n"))
	c.notify("textDocument/didOpen", openParams(uriB, "print -l *(.)\n"))
	deadline := 0
	for {
		reportA := decodeResult[lsp.FullDocumentDiagnosticReport](t, c.call("textDocument/diagnostic", `{"textDocument": {"uri": "`+uriA+`"}}`))
		if slices.ContainsFunc(reportA.Items, func(d lsp.Diagnostic) bool { return d.Code == "syntax" || d.Code == "dialect" }) {
			deadline++
			if deadline > 200 {
				t.Fatalf("folder a never switched to zsh: %+v", reportA.Items)
			}
			continue
		}
		break
	}
	reportB := decodeResult[lsp.FullDocumentDiagnosticReport](t, c.call("textDocument/diagnostic", `{"textDocument": {"uri": "`+uriB+`"}}`))
	if !slices.ContainsFunc(reportB.Items, func(d lsp.Diagnostic) bool { return d.Code == "syntax" || d.Code == "dialect" }) {
		t.Errorf("folder b (POSIX sh) accepted zsh glob qualifiers: %+v", reportB.Items)
	}
}

func TestWillRenameFilesRewritesSources(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"main.sh":        "#!/bin/sh\n. ./lib/util.sh\nsource \"$HOME/x.sh\"\n",
		"lib/util.sh":    "#!/usr/bin/env -S sh -e\nutil() { :; }\n",
		"bin/runner.sh":  "source ../lib/util.sh\n",
		"other/plain.sh": "echo unrelated\n",
	})
	c, _ := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", "capabilities": {"textDocument": {"diagnostic": {}}, "workspace": {"workspaceEdit": {"documentChanges": true}}}}`)

	oldURI := fileuri.FromPath(filepath.Join(root, "lib/util.sh"))
	newURI := fileuri.FromPath(filepath.Join(root, "shared/util.bash"))
	edit := decodeResult[lsp.WorkspaceEdit](t, c.call("workspace/willRenameFiles", `{"files": [{"oldUri": "`+oldURI+`", "newUri": "`+newURI+`"}]}`))
	got := map[string][]string{}
	for _, change := range edit.DocumentChanges {
		rel, _ := filepath.Rel(root, mustPath(t, change.TextDocument.URI))
		for _, e := range change.Edits {
			got[rel] = append(got[rel], e.NewText)
		}
	}
	want := map[string][]string{
		"main.sh":       {"./shared/util.bash"},
		"bin/runner.sh": {"../shared/util.bash"},
		"lib/util.sh":   {"#!/usr/bin/env -S bash -e"},
	}
	if len(got) != len(want) {
		t.Fatalf("edits = %v, want %v", got, want)
	}
	for file, texts := range want {
		if strings.Join(got[file], "|") != strings.Join(texts, "|") {
			t.Errorf("%s edits = %v, want %v", file, got[file], texts)
		}
	}

	dirEdit := decodeResult[lsp.WorkspaceEdit](t, c.call("workspace/willRenameFiles", `{"files": [{"oldUri": "`+fileuri.FromPath(filepath.Join(root, "bin"))+`", "newUri": "`+fileuri.FromPath(filepath.Join(root, "tools/bin"))+`"}]}`))
	if len(dirEdit.DocumentChanges) != 1 || dirEdit.DocumentChanges[0].Edits[0].NewText != "../../lib/util.sh" {
		t.Errorf("directory move edits = %+v", dirEdit)
	}
	none := c.call("workspace/willRenameFiles", `{"files": [{"oldUri": "`+fileuri.FromPath(filepath.Join(root, "other/plain.sh"))+`", "newUri": "`+fileuri.FromPath(filepath.Join(root, "other/renamed.sh"))+`"}]}`)
	if string(none.Result) != "null" {
		t.Errorf("unreferenced rename = %s", none.Result)
	}
}

func TestWorkspaceFolderChanges(t *testing.T) {
	t.Parallel()

	first, second := t.TempDir(), t.TempDir()
	writeFiles(t, second, map[string]string{"tool.sh": "added_folder_fn() { :; }\n"})
	c, _ := startClient(t, `{"rootUri": "`+fileuri.FromPath(first)+`", "capabilities": {"textDocument": {"diagnostic": {}}}}`)
	before := decodeResult[[]lsp.SymbolInformation](t, c.call("workspace/symbol", `{"query": "added_folder"}`))
	if len(before) != 0 {
		t.Fatalf("symbols before adding the folder = %+v", before)
	}
	c.notify("workspace/didChangeWorkspaceFolders", `{"event": {"added": [{"uri": "`+fileuri.FromPath(second)+`", "name": "second"}], "removed": [{"uri": "`+fileuri.FromPath(first)+`", "name": "first"}]}}`)
	after := decodeResult[[]lsp.SymbolInformation](t, c.call("workspace/symbol", `{"query": "added_folder"}`))
	if len(after) != 1 || after[0].Name != "added_folder_fn" {
		t.Errorf("symbols after adding the folder = %+v", after)
	}
}

func mustPath(t *testing.T, uri string) string {
	t.Helper()

	path, err := fileuri.ToPath(uri)
	if err != nil {
		t.Fatal(err)
	}
	return path
}
