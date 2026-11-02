package server

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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
	wrapperCapabilities = `"capabilities": {"general": {"positionEncodings": ["utf-16"]}, "textDocument": {"diagnostic": {}}, "workspace": {"diagnostics": {"refreshSupport": true}, "semanticTokens": {"refreshSupport": true}}}`
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
