package server

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/host/nix"
	"github.com/stubbedev/shell-lsp/internal/hostnav"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

func corpusProject(t *testing.T, name string) map[string]string {
	t.Helper()
	return corpusFiles(t, filepath.Join("..", "..", "corpus", "nix", name))
}

func corpusFiles(t testing.TB, root string) map[string]string {
	t.Helper()
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

func TestNixShellAcrossTheBoundaryThroughTheServer(t *testing.T) {
	t.Parallel()

	const name = "default.nix"
	files := map[string]string{
		name: "let\n  hello = writeShellScript \"hello\" ''\n    echo hi\n  '';\nin\n  stdenv.mkDerivation {\n    version = \"1\";\n    preBuild = ''\n      export FOO=$version\n    '';\n    buildPhase = ''\n      ${hello}/bin/hello $FOO $out\n    '';\n  }\n",
	}
	p := openNativeProject(t, nativeCapabilities, files, name)
	definition := func(sub string) []lsp.Location {
		return decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params(name, sub, 0, "")))
	}
	if got, want := definition("$version"), p.at(name, "version", 0); len(got) != 1 || got[0] != want {
		t.Errorf("definition of $version = %+v, want %+v", got, want)
	}
	if got, want := definition("$FOO"), p.at(name, "FOO", 0); len(got) != 1 || got[0] != want {
		t.Errorf("definition of $FOO = %+v, want %+v", got, want)
	}
	if got, want := definition("bin/hello"), p.at(name, "hello", 0); len(got) != 1 || got[0] != want {
		t.Errorf("definition of the program = %+v, want %+v", got, want)
	}
	hover := decodeResult[*lsp.Hover](t, p.c.call("textDocument/hover", p.params(name, "$out", 0, "")))
	if hover == nil || !strings.Contains(hover.Contents.Value, "store path") {
		t.Errorf("hover of $out = %+v", hover)
	}
	script := decodeResult[*lsp.Hover](t, p.c.call("textDocument/hover", p.params(name, "hello}", 0, "")))
	if script == nil || !strings.Contains(script.Contents.Value, "echo hi") {
		t.Errorf("hover of a script = %+v", script)
	}
}

func TestNixWrapperAcrossFilesThroughTheServer(t *testing.T) {
	t.Parallel()

	const probes = "modules/probes.nix"
	p := openNativeProject(t, nativeCapabilities, corpusProject(t, "dendritic"), probes)
	definition := decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params(probes, "$expected", 0, "")))
	if want := []lsp.Location{p.at(probes, "expected", 0)}; !slices.Equal(definition, want) {
		t.Errorf("definition = %+v, want %+v", definition, want)
	}
	references := decodeResult[[]lsp.Location](t, p.c.call("textDocument/references", p.params(probes, "expected", 0, `, "context": {"includeDeclaration": true}`)))
	if len(references) != 2 {
		t.Errorf("references = %+v", references)
	}
}

const (
	wrapperCapabilities = `"capabilities": {"general": {"positionEncodings": ["utf-16"]}, "textDocument": {"diagnostic": {}, "codeAction": {"codeActionLiteralSupport": {"codeActionKind": {"valueSet": ["quickfix"]}}}}, "workspace": {"diagnostics": {"refreshSupport": true}, "semanticTokens": {"refreshSupport": true}}}`
	wrapperProbes       = "modules/probes.nix"
	wrapperBuilder      = "modules/core/builders/probe.nix"
	wrapperVariable     = "expected variable"
)

func openWrapperProject(t *testing.T, quiet time.Duration) (*nativeProject, *Server) {
	t.Helper()
	files := corpusProject(t, "dendritic")
	const quoted = `test -e "$expected"`
	if !strings.Contains(files[wrapperProbes], quoted) {
		t.Fatalf("%s no longer holds %q", wrapperProbes, quoted)
	}
	files[wrapperProbes] = strings.Replace(files[wrapperProbes], quoted, `ls $expected`, 1)
	var srv *Server
	p := openNativeProjectWith(t, func(s *Server) {
		srv = s
		s.nixEval.strings.quiet = quiet
	}, wrapperCapabilities, files, wrapperProbes)
	return p, srv
}

func (p *nativeProject) awaitNixStringRefresh() {
	p.t.Helper()
	p.c.answerServerRequest(diagnosticRefreshMethod)
	p.c.answerServerRequest(semanticTokensRefreshMethod)
}

func (p *nativeProject) wrapperFinding() (lsp.Diagnostic, bool) {
	p.t.Helper()
	items := pullDiagnostics(p.t, p.c, p.uri(wrapperProbes))
	i := slices.IndexFunc(items, func(d lsp.Diagnostic) bool { return d.Code == "SC2086" })
	if i < 0 {
		return lsp.Diagnostic{}, false
	}
	return items[i], true
}

func (p *nativeProject) wrapperTokens() []string {
	p.t.Helper()
	data := decodeResult[lsp.SemanticTokens](p.t, p.c.call("textDocument/semanticTokens/full", `{"textDocument": {"uri": "`+p.uri(wrapperProbes)+`"}}`)).Data
	return texts(spellTokens(p.t, p.files[wrapperProbes], "utf-16", data))
}

func holdsWrapperVariable(tokens []string) bool {
	return slices.ContainsFunc(tokens, func(token string) bool { return strings.HasPrefix(token, wrapperVariable) })
}

func TestNixWrapperStringInAnotherFileIsShellAfterLinking(t *testing.T) {
	t.Parallel()

	p, _ := openWrapperProject(t, time.Millisecond)
	p.awaitNixStringRefresh()
	finding, found := p.wrapperFinding()
	if !found {
		t.Fatal("the proven string has no shell finding")
	}
	if want := p.at(wrapperProbes, "$expected", 0).Range.Start; finding.Range.Start != want {
		t.Errorf("the finding starts at %+v, want %+v", finding.Range.Start, want)
	}
	if got := p.wrapperTokens(); !holdsWrapperVariable(got) {
		t.Errorf("no shell token for the variable in %q", got)
	}
}

func TestNixWrapperStringFindingsCanBeFixed(t *testing.T) {
	t.Parallel()

	p, _ := openWrapperProject(t, time.Millisecond)
	p.awaitNixStringRefresh()
	finding, found := p.wrapperFinding()
	if !found {
		t.Fatal("the proven string has no shell finding")
	}
	r := finding.Range
	params := fmt.Sprintf(`{"textDocument": {"uri": %q}, "range": {"start": {"line": %d, "character": %d}, "end": {"line": %d, "character": %d}}, "context": {"diagnostics": []}}`, p.uri(wrapperProbes), r.Start.Line, r.Start.Character, r.End.Line, r.End.Character)
	fixes := decodeResult[[]lsp.CodeAction](t, p.c.call("textDocument/codeAction", params))
	if !slices.ContainsFunc(fixes, func(a lsp.CodeAction) bool { return strings.Contains(a.Title, "Double-quote") && a.Edit != nil }) {
		titles := make([]string, len(fixes))
		for i, a := range fixes {
			titles[i] = a.Title
		}
		t.Errorf("no quoting fix among %q", titles)
	}
}

func TestNixDiagnosticsAndTokensAloneBuildNoProject(t *testing.T) {
	t.Parallel()

	p, srv := openWrapperProject(t, time.Hour)
	if _, found := p.wrapperFinding(); found {
		t.Error("a diagnostics pass proved a string without linking")
	}
	if got := p.wrapperTokens(); holdsWrapperVariable(got) {
		t.Errorf("a tokens pass proved a string without linking: %q", got)
	}
	srv.graphs.mu.Lock()
	graphs := len(srv.graphs.entries)
	srv.graphs.mu.Unlock()
	if graphs != 0 {
		t.Errorf("diagnostics and tokens built %d import graphs", graphs)
	}
	if _, _, held := srv.nixEval.strings.of(p.uri(wrapperProbes)); held {
		t.Error("diagnostics and tokens published a proof")
	}
}

func TestNixWrapperStringStopsBeingShellWhenTheOverlayStopsProvingIt(t *testing.T) {
	t.Parallel()

	p, srv := openWrapperProject(t, time.Millisecond)
	p.awaitNixStringRefresh()
	if _, found := p.wrapperFinding(); !found {
		t.Fatal("the proven string has no shell finding")
	}
	const writer = "final.runCommand"
	if !strings.Contains(p.files[wrapperBuilder], writer) {
		t.Fatalf("%s no longer holds %q", wrapperBuilder, writer)
	}
	stopped := strings.Replace(p.files[wrapperBuilder], writer, "final.describe", 1)
	p.c.notify("textDocument/didOpen", openLanguageParams(p.uri(wrapperBuilder), "", stopped))
	p.awaitNixStringRefresh()
	if _, found := p.wrapperFinding(); found {
		t.Error("a string no writer reaches keeps its shell finding")
	}
	if got := p.wrapperTokens(); holdsWrapperVariable(got) {
		t.Errorf("a string no writer reaches keeps its shell tokens: %q", got)
	}
	if _, _, held := srv.nixEval.strings.of(p.uri(wrapperProbes)); held {
		t.Error("a proof that no longer holds stays in memory")
	}
}

func TestNixOffersToUpdateAnInputUnderTheCursor(t *testing.T) {
	t.Parallel()

	const name = "flake.nix"
	files := map[string]string{name: "{\n  inputs.nixpkgs.url = \"github:nixos/nixpkgs\";\n  outputs = _: { };\n}\n"}
	p := openNativeProject(t, `"capabilities": {"textDocument": {"diagnostic": {}, "codeAction": {"codeActionLiteralSupport": {"codeActionKind": {"valueSet": ["quickfix"]}}}}}`, files, name)
	r := p.at(name, "nixpkgs", 0).Range
	params := fmt.Sprintf(`{"textDocument": {"uri": %q}, "range": {"start": {"line": %d, "character": %d}, "end": {"line": %d, "character": %d}}, "context": {"diagnostics": []}}`, p.uri(name), r.Start.Line, r.Start.Character, r.End.Line, r.End.Character)
	for _, action := range decodeResult[[]lsp.CodeAction](t, p.c.call("textDocument/codeAction", params)) {
		if action.Title == "Update input nixpkgs" {
			if action.Command == nil || action.Command.Command != commandRunRecipe {
				t.Errorf("the action = %+v", action)
			}
			return
		}
	}
	t.Error("no action updates the input")
}

func TestNixWrapperStringHoverExplainsItsFinding(t *testing.T) {
	t.Parallel()

	p, _ := openWrapperProject(t, time.Millisecond)
	p.awaitNixStringRefresh()
	if _, found := p.wrapperFinding(); !found {
		t.Fatal("the proven string has no shell finding")
	}
	hover := decodeResult[*lsp.Hover](t, p.c.call("textDocument/hover", p.params(wrapperProbes, "$expected", 0, "")))
	if hover == nil || !strings.Contains(hover.Contents.Value, "SC2086") {
		t.Fatalf("hover of the finding = %+v", hover)
	}
	if !strings.Contains(hover.Contents.Value, "expected") {
		t.Errorf("hover lost what the variable is: %s", hover.Contents.Value)
	}
}

func TestNixWrapperStringShellHoverWithoutASymbol(t *testing.T) {
	t.Parallel()

	p, _ := openWrapperProject(t, time.Millisecond)
	p.awaitNixStringRefresh()
	if _, found := p.wrapperFinding(); !found {
		t.Fatal("the proven string has no shell finding")
	}
	hover := decodeResult[*lsp.Hover](t, p.c.call("textDocument/hover", p.params(wrapperProbes, "touch", 0, "")))
	if hover == nil || hover.Range == nil {
		t.Fatalf("hover of a command in the proven string = %+v", hover)
	}
	if want := p.at(wrapperProbes, "touch", 0).Range; *hover.Range != want {
		t.Errorf("hover range = %+v, want %+v", *hover.Range, want)
	}
}

func TestNixWrapperStringOfAClosedFileInWorkspaceDiagnostics(t *testing.T) {
	t.Parallel()

	files := corpusProject(t, "dendritic")
	const quoted = `test -e "$expected"`
	files[wrapperProbes] = strings.Replace(files[wrapperProbes], quoted, `ls $expected`, 1)
	p := openNativeProjectWith(t, func(s *Server) { s.nixEval.strings.quiet = time.Hour }, wrapperCapabilities, files, "flake.nix")
	report := decodeResult[lsp.WorkspaceDiagnosticReport](t, p.c.call("workspace/diagnostic", `{"previousResultIds": []}`))
	i := slices.IndexFunc(report.Items, func(item lsp.WorkspaceDocumentDiagnosticReport) bool { return item.URI == p.uri(wrapperProbes) })
	if i < 0 {
		t.Fatalf("the closed file is not reported among %d items", len(report.Items))
	}
	if got := codes(report.Items[i].Items); !slices.Contains(got, "SC2086") {
		t.Errorf("closed file findings = %q, want the one of its proven string", got)
	}
	var previous strings.Builder
	previous.WriteString(`{"previousResultIds": [`)
	for n, item := range report.Items {
		if n > 0 {
			previous.WriteString(",")
		}
		fmt.Fprintf(&previous, `{"uri": %q, "value": %q}`, item.URI, item.ResultID)
	}
	previous.WriteString("]}")
	again := decodeResult[lsp.WorkspaceDiagnosticReport](t, p.c.call("workspace/diagnostic", previous.String()))
	for _, item := range again.Items {
		if item.Kind != lsp.ReportUnchanged {
			t.Errorf("%s changed without an edit: %+v", item.URI, item.Items)
		}
	}
}

func TestNixWrapperStringFixAllFixesIt(t *testing.T) {
	t.Parallel()

	files := corpusProject(t, "dendritic")
	const quoted = `test -e "$expected"`
	files[wrapperProbes] = strings.Replace(files[wrapperProbes], quoted, "test -e \"`id -u`\"", 1)
	p := openNativeProjectWith(t, func(s *Server) { s.nixEval.strings.quiet = time.Millisecond }, `"capabilities": {"workspace": {"applyEdit": true, "workspaceEdit": {"documentChanges": true}, "diagnostics": {"refreshSupport": true}, "semanticTokens": {"refreshSupport": true}}, "textDocument": {"diagnostic": {}}}`, files, wrapperProbes)
	p.awaitNixStringRefresh()
	if got := codes(pullDiagnostics(t, p.c, p.uri(wrapperProbes))); !slices.Contains(got, "SC2006") {
		t.Fatalf("findings = %q, want the one of the proven string", got)
	}
	id := p.c.start("workspace/executeCommand", fmt.Sprintf(`{"command": "shellLsp.fixAll", "arguments": [%q]}`, p.uri(wrapperProbes)))
	applied := p.c.await("workspace/applyEdit", func(m *jsonrpc.Message) bool { return m.IsRequest() && m.Method == "workspace/applyEdit" })
	var params lsp.ApplyWorkspaceEditParams
	if err := json.Unmarshal(applied.Params, &params); err != nil {
		t.Fatal(err)
	}
	if len(params.Edit.DocumentChanges) != 1 {
		t.Fatalf("applyEdit = %+v", params)
	}
	var modern bool
	for _, edit := range params.Edit.DocumentChanges[0].Edits {
		modern = modern || edit.NewText == "$(id -u)"
	}
	if !modern {
		t.Errorf("no edit replaces the backtick: %+v", params.Edit.DocumentChanges[0].Edits)
	}
	if err := p.c.conn.Reply(applied.ID, lsp.ApplyWorkspaceEditResult{Applied: true}, nil); err != nil {
		t.Fatal(err)
	}
	if done := p.c.awaitResponse(id, "workspace/executeCommand"); done.Error != nil {
		t.Errorf("executeCommand error = %+v", done.Error)
	}
}

func TestNixWrapperStringReportsAReadItNeverSetsWhereThatMeansSomething(t *testing.T) {
	t.Parallel()

	const caller = "caller.nix"
	files := map[string]string{
		"flake.nix":  "{\n  outputs = _: { };\n}\n",
		"writer.nix": "{ writeShellScript }:\ntext: writeShellScript \"x\" text\n",
		caller:       "{ callPackage }:\nlet\n  w = callPackage ./writer.nix { };\nin\nw ''\n  echo \"$neverSet\"\n''\n",
	}
	var srv *Server
	p := openNativeProjectWith(t, func(s *Server) {
		srv = s
		s.nixEval.strings.quiet = time.Millisecond
	}, wrapperCapabilities, files, caller)
	p.awaitNixStringRefresh()
	proof, _, held := srv.nixEval.strings.of(p.uri(caller))
	if !held || len(proof.Strings) != 1 || proof.Strings[0].Ambient {
		t.Fatalf("proof = %+v, held %v", proof, held)
	}
	if got := codes(pullDiagnostics(t, p.c, p.uri(caller))); !slices.Contains(got, "SC2154") {
		t.Errorf("findings = %q, want SC2154", got)
	}
}

const embeddedTextCapabilities = `"capabilities": {"general": {"positionEncodings": ["utf-16"]}, "textDocument": {"diagnostic": {}, "signatureHelp": {}, "completion": {}}}`

func openEmbeddedText(t *testing.T, source string) *nativeProject {
	t.Helper()
	const name = "embedded.nix"
	return openNativeProject(t, embeddedTextCapabilities, map[string]string{name: source}, name)
}

func embeddedCompletion(t *testing.T, p *nativeProject, name, needle string, nth int) []string {
	t.Helper()
	at := p.at(name, needle, nth).Range.End
	return completionLabels(decodeResult[lsp.CompletionList](t, p.c.call("textDocument/completion", fmt.Sprintf(`{"textDocument": {"uri": %q}, "position": {"line": %d, "character": %d}}`, p.uri(name), at.Line, at.Character))))
}

func TestNixEmbeddedJustfileCompletesItsNames(t *testing.T) {
	t.Parallel()

	p := openEmbeddedText(t, "{ writeText }:\nwriteText \"justfile\" ''\n  deploy: bui\n    echo deploy\n  build:\n    echo build\n''\n")
	if got := embeddedCompletion(t, p, "embedded.nix", "bui", 0); !slices.Contains(got, "build") {
		t.Errorf("completion in the recipe's dependency = %q", got)
	}
}

func TestNixEmbeddedMakefileCompletesItsNames(t *testing.T) {
	t.Parallel()

	p := openEmbeddedText(t, "{ writeText }:\nwriteText \"Makefile\" ''\n  VALUE := 1\n  OTHER := $(VAL)\n  all:\n  \techo $(OTHER)\n''\n")
	if got := embeddedCompletion(t, p, "embedded.nix", "$(VAL", 0); !slices.Contains(got, "VALUE") {
		t.Errorf("completion in a reference = %q", got)
	}
}

func TestNixEmbeddedDockerfileCompletesItsNames(t *testing.T) {
	t.Parallel()

	p := openEmbeddedText(t, "{ writeText }:\nwriteText \"Dockerfile\" ''\n  FROM alpine:3.20 AS build\n  RUN true\n  FROM alpine:3.20\n  COPY --from=bu /a /b\n''\n")
	if got := embeddedCompletion(t, p, "embedded.nix", "--from=bu", 0); !slices.Contains(got, "build") {
		t.Errorf("completion of a stage = %q", got)
	}
}

func TestNixEmbeddedJustfileSignatureHelp(t *testing.T) {
	t.Parallel()

	p := openEmbeddedText(t, "{ writeText }:\nwriteText \"justfile\" ''\n  value := replace(\"a\", \n  all:\n    echo {{value}}\n''\n")
	at := p.at("embedded.nix", `"a", `, 0).Range.End
	help := decodeResult[*lsp.SignatureHelp](t, p.c.call("textDocument/signatureHelp", fmt.Sprintf(`{"textDocument": {"uri": %q}, "position": {"line": %d, "character": %d}}`, p.uri("embedded.nix"), at.Line, at.Character)))
	if help == nil || len(help.Signatures) != 1 || !strings.HasPrefix(help.Signatures[0].Label, "replace(") {
		t.Fatalf("signature help = %+v", help)
	}
	if help.ActiveParameter == nil || *help.ActiveParameter != 1 {
		t.Errorf("active parameter = %v, want the second", help.ActiveParameter)
	}
}

func TestNixEmbeddedDockerfileGetsHadolintRules(t *testing.T) {
	t.Parallel()

	p := openEmbeddedText(t, "{ writeText, version }:\nwriteText \"Dockerfile\" ''\n  FROM alpine\n  # hadolint ignore=DL3059\n  RUN cd /tmp\n  FROM alpine:${version}\n''\n")
	items := pullDiagnostics(t, p.c, p.uri("embedded.nix"))
	got := codes(items)
	for _, want := range []string{"DL3006", "DL3003"} {
		if !slices.Contains(got, want) {
			t.Errorf("findings = %q, want %s", got, want)
		}
	}
	if want := p.at("embedded.nix", "alpine", 0).Range.Start; !slices.ContainsFunc(items, func(d lsp.Diagnostic) bool { return d.Code == "DL3006" && d.Range.Start == want }) {
		t.Errorf("DL3006 is not at the FROM: %+v", items)
	}
	if n := len(slices.DeleteFunc(slices.Clone(got), func(code string) bool { return code != "DL3006" })); n != 1 {
		t.Errorf("a FROM whose tag is an antiquotation was reported: %q", got)
	}
}

func TestNixEmbeddedDockerfileHonoursItsPragmas(t *testing.T) {
	t.Parallel()

	p := openEmbeddedText(t, "{ writeText }:\nwriteText \"Dockerfile\" ''\n  # hadolint ignore=DL3006\n  FROM alpine\n''\n")
	if got := codes(pullDiagnostics(t, p.c, p.uri("embedded.nix"))); slices.Contains(got, "DL3006") {
		t.Errorf("findings = %q, want the pragma honoured", got)
	}
}

func TestNixEmbeddedNamesAreCheckedForUse(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, source string
		code         lint.Code
	}{
		{"a justfile variable", "{ writeText }:\nwriteText \"justfile\" ''\n  unused := \"1\"\n  all:\n    echo hi\n''\n", lint.CodeJustUnusedVariable},
		{"a Dockerfile ARG", "{ writeText }:\nwriteText \"Dockerfile\" ''\n  FROM alpine:3.20\n  ARG unused=1\n  COPY a b\n''\n", lint.CodeDockerUnusedArg},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := openEmbeddedText(t, tt.source)
			got := codes(pullDiagnostics(t, p.c, p.uri("embedded.nix")))
			if !slices.Contains(got, tt.code.String()) {
				t.Errorf("findings = %q, want %s", got, tt.code)
			}
		})
	}
}

func TestNixEmbeddedIncludes(t *testing.T) {
	t.Parallel()

	missing := lint.CodeJustMissingFile.String()
	for _, tt := range []struct {
		name, source string
		reported     bool
	}{
		{"a store file has nothing beside it", "{ writeText }:\nwriteText \"justfile\" ''\n  import 'lib.just'\n  all:\n    echo hi\n''\n", true},
		{"a placed file may have a real file beside it", "{\n  home.file.\"app/justfile\".text = ''\n    import 'lib.just'\n    all:\n      echo hi\n  '';\n}\n", false},
		{"a dynamic include is not known", "{ writeText, dir }:\nwriteText \"justfile\" ''\n  import '${dir}/lib.just'\n  all:\n    echo hi\n''\n", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := openEmbeddedText(t, tt.source)
			got := codes(pullDiagnostics(t, p.c, p.uri("embedded.nix")))
			if slices.Contains(got, missing) != tt.reported {
				t.Errorf("findings = %q, want %s reported: %v", got, missing, tt.reported)
			}
		})
	}
}

func TestNixEmbeddedIncludeResolvesToItsSibling(t *testing.T) {
	t.Parallel()

	p := openEmbeddedText(t, "{\n  home.file.\"app/justfile\".text = ''\n    import 'lib.just'\n    all: shared\n      echo hi\n  '';\n  home.file.\"app/lib.just\".text = ''\n    shared:\n      echo shared\n  '';\n}\n")
	got := codes(pullDiagnostics(t, p.c, p.uri("embedded.nix")))
	for _, unwanted := range []lint.Code{lint.CodeJustMissingFile, lint.CodeJustUnknownRecipe} {
		if slices.Contains(got, unwanted.String()) {
			t.Errorf("findings = %q hold %s, although the sibling defines it", got, unwanted)
		}
	}
}

func TestNixTextNamedNixIsAnalysedAsNix(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, source string }{
		{"writeText", "{ writeText }:\nwriteText \"gen.nix\" ''\n  let\n    kept = 1;\n    unused = 2;\n  in\n  kept\n''\n"},
		{"a home file", "{\n  home.file.\"gen.nix\".text = ''\n    let\n      kept = 1;\n      unused = 2;\n    in\n    kept\n  '';\n}\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			const name = "embedded.nix"
			p := openEmbeddedText(t, tt.source)
			items := pullDiagnostics(t, p.c, p.uri(name))
			if want := p.at(name, "unused", 0).Range.Start; !slices.ContainsFunc(items, func(d lsp.Diagnostic) bool { return d.Range.Start == want }) {
				t.Errorf("no finding on the unused binding at %+v: %+v", want, items)
			}
			definition := decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params(name, "kept", 1, "")))
			if want := []lsp.Location{p.at(name, "kept", 0)}; !slices.Equal(definition, want) {
				t.Errorf("definition = %+v, want %+v", definition, want)
			}
			data := decodeResult[lsp.SemanticTokens](t, p.c.call("textDocument/semanticTokens/full", `{"textDocument": {"uri": "`+p.uri(name)+`"}}`)).Data
			spelled := texts(spellTokens(t, tt.source, "utf-16", data))
			checkInOrder(t, spelled, []string{"let keyword", "in keyword"})
		})
	}
}

func TestNixTextNamedNixKeepsTheAntiquotationOutOfItsFindings(t *testing.T) {
	t.Parallel()

	p := openEmbeddedText(t, "{ writeText, value }:\nwriteText \"gen.nix\" ''\n  { a = ${value}; }\n''\n")
	if got := codes(pullDiagnostics(t, p.c, p.uri("embedded.nix"))); len(got) != 0 {
		t.Errorf("findings = %q, want none about the stand-in", got)
	}
}

func (p *nativeProject) linkedProject(t *testing.T, srv *Server, name string) *nix.Project {
	t.Helper()
	docs := srv.docs.View()
	snap, err := srv.snapshot(docs, p.uri(name))
	if err != nil {
		t.Fatal(err)
	}
	h, hosted := hostnav.Of(snap.File)
	if !hosted {
		t.Fatalf("%s is not a Nix file", name)
	}
	g, err := srv.group(t.Context(), docs, h)
	if err != nil {
		t.Fatal(err)
	}
	project, linked := g.Module.Links().(*nix.Project)
	if !linked {
		t.Fatalf("%s holds no project", name)
	}
	return project
}

func TestNixOpeningAnUnchangedFileKeepsTheProject(t *testing.T) {
	t.Parallel()

	const reader, setter = stalenessReader, stalenessSetter
	var srv *Server
	p := openNativeProjectWith(t, func(s *Server) { srv = s }, nativeCapabilities, corpusProject(t, "dendritic"), reader)
	if got := decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params(reader, "ownerName", 1, ""))); len(got) == 0 {
		t.Fatal("no definition before opening the other file")
	}
	before := p.linkedProject(t, srv, reader)

	p.c.notify("textDocument/didOpen", openLanguageParams(p.uri(setter), "", p.files[setter]))
	if got := decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params(reader, "ownerName", 1, ""))); len(got) == 0 {
		t.Fatal("no definition after opening the other file")
	}
	if after := p.linkedProject(t, srv, reader); after != before {
		t.Error("opening a file whose text is on disk linked the project again")
	}

	p.c.notify("textDocument/didClose", fmt.Sprintf(`{"textDocument": {"uri": %q}}`, p.uri(setter)))
	if got := decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params(reader, "ownerName", 1, ""))); len(got) == 0 {
		t.Fatal("no definition after closing the other file")
	}
	if after := p.linkedProject(t, srv, reader); after != before {
		t.Error("closing a file whose text is on disk linked the project again")
	}

	p.c.notify("textDocument/didOpen", openLanguageParams(p.uri(setter), "", p.files[setter]+"\n"))
	if got := decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params(reader, "ownerName", 1, ""))); len(got) == 0 {
		t.Fatal("no definition after opening the other file changed")
	}
	if after := p.linkedProject(t, srv, reader); after == before {
		t.Error("a file opened with other text kept the project of its old text")
	}
}

func TestNixScriptAfterARewrittenOneKeepsItsPositions(t *testing.T) {
	t.Parallel()

	const name = "scripts.nix"
	source := "{ pkgs, x }:\n{\n  a = pkgs.writeShellScript \"a\" ''\n    ${pkgs.gnused}/bin/sed s/a/b/ \"$1\"\n    echo '${x}'\n  '';\n  b = pkgs.writeShellScript \"b\" ''\n    echo 'quoted' ${x}\n    ls $1\n    cat `pwd`\n  '';\n}\n"
	p := openNativeProject(t, nativeCapabilities, map[string]string{name: source}, name)
	items := pullDiagnostics(t, p.c, p.uri(name))
	for _, want := range []struct{ code, at string }{{"SC2086", "$1\n    cat"}, {"SC2006", "`pwd`"}} {
		start := p.at(name, want.at, 0).Range.Start
		if !slices.ContainsFunc(items, func(d lsp.Diagnostic) bool { return d.Code == want.code && d.Range.Start == start }) {
			t.Errorf("no %s at %+v among %+v", want.code, start, items)
		}
	}
	data := decodeResult[lsp.SemanticTokens](t, p.c.call("textDocument/semanticTokens/full", `{"textDocument": {"uri": "`+p.uri(name)+`"}}`)).Data
	checkInOrder(t, texts(spellTokens(t, source, "utf-16", data)), []string{"sed function external", "ls function external"})
}
