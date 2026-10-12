package server

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/diagnostics"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

const batsFile = "#!/usr/bin/env bats\n" +
	"setup() {\n" +
	"\tx=1\n" +
	"}\n" +
	"@test \"adds\" { :; }\n" +
	"@test \"compares\" {\n" +
	"\t[ \"$x\" = 2 ]\n" +
	"}\n" +
	"@test \"later\" { skip; }\n" +
	"@test \"uses $x\" { :; }\n"

const fakeBats = `#!/bin/sh
{ pwd; printf '%s\n' "$@"; } > "$0.argv"
case "$*" in
*--filter*)
	printf '%s\n' '1..1' 'ok 1 compares'
	exit 0
	;;
esac
printf '%s\n' '1..3' 'ok 1 adds' 'not ok 2 compares' '# (in test file suite.bats, line 7)' "#   \` + "`" + `[ \"\$x\" = 2 ]' failed" 'ok 3 later # skip not yet'
exit 1
`

func writeRunner(t *testing.T, path string) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("the fake bats runner is a POSIX shell script")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(fakeBats), 0o700); err != nil {
		t.Fatal(err)
	}
}

func batsSuiteOnDisk(t *testing.T) (string, string) {
	t.Helper()

	root := t.TempDir()
	path := filepath.Join(root, "suite.bats")
	if err := os.WriteFile(path, []byte(batsFile), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, fileuri.FromPath(path)
}

func runCommand(t *testing.T, lens lsp.CodeLens) string {
	t.Helper()

	if lens.Command == nil || lens.Command.Command != commandRunBats || len(lens.Command.Arguments) != 1 {
		t.Fatalf("lens %+v does not run bats", lens)
	}
	return `{"command": "` + commandRunBats + `", "arguments": [` + string(lens.Command.Arguments[0]) + `]}`
}

func batsDiagnostics(items []lsp.Diagnostic) []lsp.Diagnostic {
	return slices.DeleteFunc(slices.Clone(items), func(d lsp.Diagnostic) bool { return d.Source != diagnostics.BatsSource })
}

func TestBatsCodeLenses(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/suite.bats", batsFile))
	lenses := decodeResult[[]lsp.CodeLens](t, c.call("textDocument/codeLens", `{"textDocument": {"uri": "file:///w/suite.bats"}}`))
	var titles []string
	for _, lens := range lenses {
		if lens.Command != nil {
			titles = append(titles, fmt.Sprintf("%s@%d", lens.Command.Title, lens.Range.Start.Line))
		}
	}
	if strings.Join(titles, " ") != "▶ Run file@0 ▶ Run test@4 ▶ Run test@5 ▶ Run test@8 ▶ Run matching tests@9" || len(lenses) != 6 || lenses[5].Data == nil {
		t.Fatalf("lenses = %v (%d in all)", titles, len(lenses))
	}
	var args batsRunArgs
	if err := json.Unmarshal(lenses[2].Command.Arguments[0], &args); err != nil || args.URI != "file:///w/suite.bats" || args.Test == nil || *args.Test != "compares" {
		t.Errorf("run test arguments = %+v, %v", args, err)
	}
	raw, err := json.Marshal(lenses[0])
	if err != nil {
		t.Fatal(err)
	}
	if resolved := decodeResult[lsp.CodeLens](t, c.call("codeLens/resolve", string(raw))); resolved.Command == nil || resolved.Command.Title != "▶ Run file" {
		t.Errorf("resolving a run lens = %+v, want it unchanged", resolved)
	}

	c.notify("textDocument/didOpen", openParams("file:///w/empty.bats", "#!/usr/bin/env bats\n"))
	if none := c.call("textDocument/codeLens", `{"textDocument": {"uri": "file:///w/empty.bats"}}`); string(none.Result) != "[]" {
		t.Errorf("lenses of a file without tests = %s, want []", none.Result)
	}

	c.notify("textDocument/didOpen", openParams("file:///w/plain.sh", "#!/bin/bash\nsetup() { :; }\n"))
	for _, lens := range decodeResult[[]lsp.CodeLens](t, c.call("textDocument/codeLens", `{"textDocument": {"uri": "file:///w/plain.sh"}}`)) {
		if lens.Command != nil {
			t.Errorf("a plain script got a run lens: %+v", lens)
		}
	}
}

func TestRunBatsFromPathPublishesResults(t *testing.T) {
	bin := t.TempDir()
	runner := filepath.Join(bin, "bats")
	writeRunner(t, runner)
	t.Setenv("PATH", bin)
	root, uri := batsSuiteOnDisk(t)
	c, _ := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", "capabilities": {"textDocument": {"diagnostic": {}}, "window": {"workDoneProgress": true}, "workspace": {"diagnostics": {"refreshSupport": true}}}}`)
	c.notify("textDocument/didOpen", openParams(uri, batsFile))
	lenses := decodeResult[[]lsp.CodeLens](t, c.call("textDocument/codeLens", `{"textDocument": {"uri": "`+uri+`"}}`))

	id := c.start("workspace/executeCommand", runCommand(t, lenses[0]))
	c.answerServerRequest("window/workDoneProgress/create")
	result := decodeResult[map[string]any](t, c.awaitResponse(id, "workspace/executeCommand"))
	if want := map[string]any{"passed": 1.0, "failed": 1.0, "skipped": 1.0, "todo": 0.0, "summary": "1 passed, 1 failed, 1 skipped"}; !maps.Equal(result, want) {
		t.Errorf("result = %+v", result)
	}
	c.awaitNotification("$/progress", func(raw json.RawMessage) bool {
		return strings.Contains(string(raw), `"message":"2/3 tests","percentage":66`)
	})
	c.awaitNotification("$/progress", func(raw json.RawMessage) bool {
		return strings.Contains(string(raw), `"kind":"end","message":"1 passed, 1 failed, 1 skipped"`)
	})
	shown := c.awaitNotification("window/showMessage", func(json.RawMessage) bool { return true })
	if !strings.Contains(string(shown), `"type":1,"message":"bats suite.bats: 1 passed, 1 failed, 1 skipped"`) {
		t.Errorf("summary = %s", shown)
	}
	c.answerServerRequest("workspace/diagnostic/refresh")
	argv, err := os.ReadFile(runner + ".argv")
	if err != nil {
		t.Fatal(err)
	}
	if want := root + "\n--tap\nsuite.bats\n"; string(argv) != want {
		t.Errorf("cwd and argv = %q, want %q", argv, want)
	}

	report := decodeResult[lsp.FullDocumentDiagnosticReport](t, c.call("textDocument/diagnostic", `{"textDocument": {"uri": "`+uri+`"}}`))
	failures := batsDiagnostics(report.Items)
	wantRange := lsp.Range{Start: lsp.Position{Line: 6, Character: 1}, End: lsp.Position{Line: 6, Character: 13}}
	if len(failures) != 1 || failures[0].Range != wantRange || failures[0].Severity != lsp.SeverityError || failures[0].Message != "test failed: compares\n  `[ \"$x\" = 2 ]' failed" {
		t.Fatalf("bats diagnostics = %+v", failures)
	}

	id = c.start("workspace/executeCommand", runCommand(t, lenses[2]))
	c.answerServerRequest("window/workDoneProgress/create")
	if single := decodeResult[map[string]any](t, c.awaitResponse(id, "workspace/executeCommand")); single["passed"] != 1.0 || single["failed"] != 0.0 {
		t.Errorf("single test result = %+v", single)
	}
	c.answerServerRequest("workspace/diagnostic/refresh")
	if argv, err = os.ReadFile(runner + ".argv"); err != nil || !strings.Contains(string(argv), "\n--filter\n^compares$\n") {
		t.Errorf("single test argv = %q, %v", argv, err)
	}
	again := decodeResult[lsp.FullDocumentDiagnosticReport](t, c.call("textDocument/diagnostic", `{"textDocument": {"uri": "`+uri+`"}, "previousResultId": "`+report.ResultID+`"}`))
	if again.Kind != lsp.ReportFull || len(batsDiagnostics(again.Items)) != 0 {
		t.Errorf("after the test passed: %+v", again)
	}
}

func TestRunBatsRefusesFilesThatAreNotBats(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	c, _ := startClient(t, `{"capabilities": {}}`)
	for _, doc := range []struct{ uri, language, text string }{
		{"file:///w/Makefile", "makefile", "all:\n\techo hi\n"},
		{"file:///w/a.sh", "shellscript", "#!/bin/sh\necho hi\n"},
	} {
		c.notify("textDocument/didOpen", openLanguageParams(doc.uri, doc.language, doc.text))
		got := c.call("workspace/executeCommand", `{"command": "`+commandRunBats+`", "arguments": [{"uri": "`+doc.uri+`"}]}`)
		if got.Error == nil || got.Error.Code != jsonrpc.CodeInvalidParams || !strings.Contains(got.Error.Message, doc.uri+" is not a Bats file") {
			t.Errorf("running bats on %s = %+v, want an error saying it is not a Bats file", doc.uri, got)
		}
	}
}

func TestRunBatsWithProjectRunnerPushesDiagnostics(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root, uri := batsSuiteOnDisk(t)
	c, _ := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", "capabilities": {}}`)
	c.notify("textDocument/didOpen", openParams(uri, batsFile))

	missing := c.call("workspace/executeCommand", `{"command": "`+commandRunBats+`", "arguments": [{"uri": "`+uri+`"}]}`)
	if missing.Error == nil || missing.Error.Code != jsonrpc.CodeRequestFailed || !strings.Contains(missing.Error.Message, "bats not found: looked for test/bats/bin/bats, node_modules/.bin/bats, test/libs/bats/bin/bats under "+root) {
		t.Fatalf("without bats: %+v", missing.Error)
	}
	unknown := c.call("workspace/executeCommand", `{"command": "`+commandRunBats+`", "arguments": [{"uri": "`+uri+`", "test": "uses $x"}]}`)
	if unknown.Error == nil || unknown.Error.Code != jsonrpc.CodeInvalidParams {
		t.Fatalf("running a test whose name is only known at run time: %+v", unknown)
	}

	writeRunner(t, filepath.Join(root, "test", "bats", "bin", "bats"))
	decodeResult[map[string]any](t, c.call("workspace/executeCommand", `{"command": "`+commandRunBats+`", "arguments": [{"uri": "`+uri+`"}]}`))
	c.awaitNotification("textDocument/publishDiagnostics", func(raw json.RawMessage) bool {
		params, err := lsp.DecodeParams[lsp.PublishDiagnosticsParams](raw)
		return err == nil && len(batsDiagnostics(params.Diagnostics)) == 1 && batsDiagnostics(params.Diagnostics)[0].Range.Start.Line == 6
	})
}

func TestBatsFailuresFollowTheirTest(t *testing.T) {
	bin := t.TempDir()
	writeRunner(t, filepath.Join(bin, "bats"))
	t.Setenv("PATH", bin)
	root, uri := batsSuiteOnDisk(t)
	c, _ := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", "capabilities": {"textDocument": {"diagnostic": {}}}}`)
	unsaved := strings.Replace(batsFile, "@test \"adds\"", "# an unsaved note\n@test \"adds\"", 1)
	c.notify("textDocument/didOpen", openParams(uri, unsaved))
	c.call("workspace/executeCommand", `{"command": "`+commandRunBats+`", "arguments": [{"uri": "`+uri+`"}]}`)

	failureLine := func() []uint32 {
		report := decodeResult[lsp.FullDocumentDiagnosticReport](t, c.call("textDocument/diagnostic", `{"textDocument": {"uri": "`+uri+`"}}`))
		var lines []uint32
		for _, d := range batsDiagnostics(report.Items) {
			lines = append(lines, d.Range.Start.Line)
		}
		return lines
	}
	if got := failureLine(); !slices.Equal(got, []uint32{7}) {
		t.Fatalf("failure lines = %v, want the assertion's line in the unsaved buffer (bats ran the file on disk)", got)
	}
	c.notify("textDocument/didChange", `{"textDocument": {"uri": "`+uri+`", "version": 2}, "contentChanges": [{"range": {"start": {"line": 0, "character": 0}, "end": {"line": 0, "character": 0}}, "text": "\n\n"}]}`)
	if got := failureLine(); !slices.Equal(got, []uint32{9}) {
		t.Errorf("after inserting two lines above: failure lines = %v, want the assertion to keep its diagnostic", got)
	}
	removed := strings.Replace(unsaved, "@test \"compares\" {\n\t[ \"$x\" = 2 ]\n}\n", "", 1)
	c.notify("textDocument/didChange", fmt.Sprintf(`{"textDocument": {"uri": %q, "version": 3}, "contentChanges": [{"text": %q}]}`, uri, removed))
	if got := failureLine(); len(got) != 0 {
		t.Errorf("after deleting the failed test: failure lines = %v, want none", got)
	}
}
