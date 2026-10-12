package server

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/shellcheckrc"
)

func diagnosticCodes(items []lsp.Diagnostic) []string {
	out := make([]string, len(items))
	for i, d := range items {
		out[i] = d.Code
	}
	return out
}

func TestLintFollowsTheNearestShellcheckrc(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rc := filepath.Join(dir, shellcheckrc.FileName)
	writeTestFile(t, rc, "disable=SC2086\n")
	c, _ := startClient(t, fmt.Sprintf(`{"rootUri": %q, "capabilities": {"textDocument": {"diagnostic": {}}, "workspace": {"didChangeWatchedFiles": {"dynamicRegistration": true}}}}`, fileuri.FromPath(dir)))
	script := fileuri.FromPath(filepath.Join(dir, "sub", "a.sh"))
	c.notify("textDocument/didOpen", openParams(script, "#!/bin/bash\necho $1 `date`\n"))
	if got := diagnosticCodes(pullDiagnostics(t, c, script)); slices.Contains(got, "SC2086") || !slices.Contains(got, "SC2006") {
		t.Errorf("codes = %v, want SC2006 but not SC2086, which .shellcheckrc disables", got)
	}

	writeTestFile(t, rc, "disable=SC2006\n")
	c.notify("workspace/didChangeWatchedFiles", fmt.Sprintf(`{"changes": [{"uri": %q, "type": 2}]}`, fileuri.FromPath(rc)))
	if got := diagnosticCodes(pullDiagnostics(t, c, script)); !slices.Contains(got, "SC2086") || slices.Contains(got, "SC2006") {
		t.Errorf("codes after the change = %v, want SC2086 back and SC2006 gone", got)
	}

	writeTestFile(t, rc, "disable=nope\n")
	c.notify("workspace/didChangeWatchedFiles", fmt.Sprintf(`{"changes": [{"uri": %q, "type": 2}]}`, fileuri.FromPath(rc)))
	if got := diagnosticCodes(pullDiagnostics(t, c, script)); !slices.Contains(got, "SC2086") || !slices.Contains(got, "SC2006") {
		t.Errorf("codes with a broken .shellcheckrc = %v, want both findings", got)
	}
	c.await("a notice about the broken .shellcheckrc", func(m *jsonrpc.Message) bool {
		return m.Method == "window/logMessage" && strings.Contains(string(m.Params), shellcheckrc.FileName) && strings.Contains(string(m.Params), "nope")
	})
}

func TestLintSettingsMatchCheck(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "sh", shellcheckrc.FileName), "shell=sh\n")
	c, _ := startClient(t, fmt.Sprintf(`{"rootUri": %q, "capabilities": {"textDocument": {"diagnostic": {}}}}`, fileuri.FromPath(dir)))

	forced := fileuri.FromPath(filepath.Join(dir, "sh", "a.sh"))
	c.notify("textDocument/didOpen", openParams(forced, "#!/bin/bash\n[[ -n $1 ]] && echo hi\n"))
	if got := diagnosticCodes(pullDiagnostics(t, c, forced)); !slices.Contains(got, "SC3010") {
		t.Errorf("codes under shell=sh = %v, want SC3010 because .shellcheckrc chooses sh as check does", got)
	}
	if answer := askDialect(t, c, forced); answer.Dialect != "sh" || answer.Source != "shellcheckrc" || answer.Description != "from shell=sh in .shellcheckrc" {
		t.Errorf("dialect answer = %+v, want sh from .shellcheckrc", answer)
	}

	guessed := fileuri.FromPath(filepath.Join(dir, "script"))
	c.notify("textDocument/didOpen", openParams(guessed, "echo hi\n"))
	if got := diagnosticCodes(pullDiagnostics(t, c, guessed)); !slices.Contains(got, "SC2148") {
		t.Errorf("codes of a script that names no shell = %v, want SC2148 as check reports", got)
	}
}

func TestWorkspaceFoldersThatAreNotFilesAreReported(t *testing.T) {
	t.Parallel()

	c, srv := startClient(t, `{"workspaceFolders": [{"uri": "vscode-vfs://github/a/b", "name": "remote"}], "capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.await("a notice about the remote folder", func(m *jsonrpc.Message) bool {
		return m.Method == "window/logMessage" && strings.Contains(string(m.Params), "vscode-vfs://github/a/b")
	})
	if roots := srv.workspace.Roots(); len(roots) != 0 {
		t.Errorf("roots = %v, want none", roots)
	}
}
