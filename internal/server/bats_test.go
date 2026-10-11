package server

import (
	"encoding/json"
	"fmt"
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
	if strings.Join(titles, " ") != "▶ Run file@0 ▶ Run test@4 ▶ Run test@5 ▶ Run test@8" || len(lenses) != 5 || lenses[4].Data == nil {
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
	result := decodeResult[batsRunResult](t, c.awaitResponse(id, "workspace/executeCommand"))
	if result.Passed != 1 || result.Failed != 1 || result.Skipped != 1 || result.Summary != "1 passed, 1 failed, 1 skipped" {
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
	if want := root + "\n--tap\n" + filepath.Join(root, "suite.bats") + "\n"; string(argv) != want {
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
	if single := decodeResult[batsRunResult](t, c.awaitResponse(id, "workspace/executeCommand")); single.Passed != 1 || single.Failed != 0 {
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
	decodeResult[batsRunResult](t, c.call("workspace/executeCommand", `{"command": "`+commandRunBats+`", "arguments": [{"uri": "`+uri+`"}]}`))
	c.awaitNotification("textDocument/publishDiagnostics", func(raw json.RawMessage) bool {
		params, err := lsp.DecodeParams[lsp.PublishDiagnosticsParams](raw)
		return err == nil && len(batsDiagnostics(params.Diagnostics)) == 1 && batsDiagnostics(params.Diagnostics)[0].Range.Start.Line == 6
	})
}
