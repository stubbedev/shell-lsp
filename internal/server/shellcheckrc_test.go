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
