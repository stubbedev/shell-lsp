package server

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/shellcheckrc"
)

func actionsOnFirst(t *testing.T, c *testClient, uri, code string) []lsp.CodeAction {
	t.Helper()

	report := pullDiagnostics(t, c, uri)
	i := slices.IndexFunc(report, func(d lsp.Diagnostic) bool { return d.Code == code })
	if i < 0 {
		t.Fatalf("no %s among %+v", code, report)
	}
	diag, err := json.Marshal(report[i : i+1])
	if err != nil {
		t.Fatal(err)
	}
	r, err := json.Marshal(report[i].Range)
	if err != nil {
		t.Fatal(err)
	}
	return codeActions(t, c, fmt.Sprintf(`{"textDocument": {"uri": %q}, "range": %s, "context": {"diagnostics": %s}}`, uri, r, diag))
}

func TestFixAllOfACodeResolvesLazily(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{`+actionCapabilities+`}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.sh", "#!/bin/bash\na=`date`\nb=`pwd`\necho \"$a$b\"\n"))
	fixAll, ok := titled(actionsOnFirst(t, c, "file:///w/a.sh", "SC2006"), "Fix all SC2006 in this file (2)")
	if !ok || fixAll.Edit != nil || len(fixAll.Data) == 0 || fixAll.Kind != "quickfix" {
		t.Fatalf("fix-all = %+v, want a lazy quick fix", fixAll)
	}
	raw, err := json.Marshal(fixAll)
	if err != nil {
		t.Fatal(err)
	}
	resolved := decodeResult[lsp.CodeAction](t, c.call("codeAction/resolve", string(raw)))
	if resolved.Edit == nil || len(resolved.Edit.DocumentChanges) != 1 {
		t.Fatalf("resolved = %+v", resolved.Edit)
	}
	var texts []string
	for _, e := range resolved.Edit.DocumentChanges[0].Edits {
		texts = append(texts, e.NewText)
	}
	if !slices.Equal(texts, []string{"$(date)", "$(pwd)"}) {
		t.Errorf("resolved edits = %q, want both command substitutions rewritten", texts)
	}
}

const resourceCapabilities = `"capabilities": {"textDocument": {"diagnostic": {}, "codeAction": {"codeActionLiteralSupport": {}}}, "workspace": {"workspaceEdit": {"documentChanges": true, "resourceOperations": ["create"], "changeAnnotationSupport": {}}}}`

func TestIgnoreInWorkspaceCreatesTheSettingsFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, _ := startClient(t, fmt.Sprintf(`{"rootUri": %q, %s}`, fileuri.FromPath(dir), resourceCapabilities))
	script := fileuri.FromPath(filepath.Join(dir, "bin", "a.sh"))
	c.notify("textDocument/didOpen", openParams(script, "#!/bin/bash\necho $1\n"))
	ignore, ok := titled(actionsOnFirst(t, c, script, "SC2086"), "Ignore SC2086 in this workspace")
	if !ok || ignore.Edit == nil {
		t.Fatalf("workspace ignore = %+v", ignore)
	}
	rc := fileuri.FromPath(filepath.Join(dir, shellcheckrc.FileName))
	edit := ignore.Edit
	if len(edit.CreateFiles) != 1 || edit.CreateFiles[0].URI != rc || len(edit.DocumentChanges) != 1 || edit.DocumentChanges[0].TextDocument.URI != rc {
		t.Fatalf("edit = %+v, want %s created and then written", edit, rc)
	}
	if edits := edit.DocumentChanges[0].Edits; len(edits) != 1 || edits[0].NewText != "disable=SC2086\n" || edits[0].AnnotationID == "" {
		t.Errorf("rc edits = %+v", edits)
	}
	if a := edit.ChangeAnnotations[edit.CreateFiles[0].AnnotationID]; !a.NeedsConfirmation {
		t.Errorf("annotations = %+v, want the creation to need confirmation", edit.ChangeAnnotations)
	}
	raw, err := json.Marshal(edit)
	if err != nil {
		t.Fatal(err)
	}
	if create, write := strings.Index(string(raw), `"kind":"create"`), strings.Index(string(raw), `"textDocument"`); create < 0 || write < create {
		t.Errorf("JSON %s, want the create operation before the text edit", raw)
	}
}

func TestIgnoreInWorkspaceExtendsTheNearestSettingsFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "bin", "shellcheckrc"), "shell=bash\ndisable=SC2034\n")
	c, _ := startClient(t, fmt.Sprintf(`{"rootUri": %q, "capabilities": {"textDocument": {"diagnostic": {}, "codeAction": {"codeActionLiteralSupport": {}}}}}`, fileuri.FromPath(dir)))
	script := fileuri.FromPath(filepath.Join(dir, "bin", "a.sh"))
	c.notify("textDocument/didOpen", openParams(script, "#!/bin/bash\necho $1\n"))
	ignore, ok := titled(actionsOnFirst(t, c, script, "SC2086"), "Ignore SC2086 in this workspace")
	if !ok || ignore.Edit == nil {
		t.Fatalf("workspace ignore = %+v", ignore)
	}
	rc := fileuri.FromPath(filepath.Join(dir, "bin", "shellcheckrc"))
	edits := ignore.Edit.Changes[rc]
	if len(ignore.Edit.CreateFiles) != 0 || len(edits) != 1 || edits[0].NewText != ",SC2086" || edits[0].Range.Start != (lsp.Position{Line: 1, Character: 14}) {
		t.Errorf("edit = %+v, want ,SC2086 appended to the existing disable line", ignore.Edit)
	}
}

func TestIgnoreInWorkspaceNeedsFileCreationSupport(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c, _ := startClient(t, fmt.Sprintf(`{"rootUri": %q, %s}`, fileuri.FromPath(dir), actionCapabilities))
	script := fileuri.FromPath(filepath.Join(dir, "a.sh"))
	c.notify("textDocument/didOpen", openParams(script, "#!/bin/bash\necho $1\n"))
	if a, ok := titled(actionsOnFirst(t, c, script, "SC2086"), "Ignore SC2086 in this workspace"); ok {
		t.Errorf("offered %+v to a client that cannot create files", a)
	}
}
