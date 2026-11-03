package server

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

type nativeProject struct {
	t     *testing.T
	root  string
	files map[string]string
	c     *testClient
}

func openNativeProject(t *testing.T, capabilities string, files map[string]string, open ...string) *nativeProject {
	t.Helper()
	return openNativeProjectWith(t, func(*Server) {}, capabilities, files, open...)
}

func openNativeProjectWith(t *testing.T, setup func(*Server), capabilities string, files map[string]string, open ...string) *nativeProject {
	t.Helper()
	root := t.TempDir()
	for name, text := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c, _ := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", `+capabilities+`}`, setup)
	p := &nativeProject{t: t, root: root, files: files, c: c}
	for _, name := range open {
		c.notify("textDocument/didOpen", openLanguageParams(p.uri(name), "", files[name]))
	}
	return p
}

func (p *nativeProject) uri(name string) string { return fileuri.FromPath(filepath.Join(p.root, name)) }

func (p *nativeProject) pos(name, sub string, nth int) (int, int) {
	p.t.Helper()
	text := p.files[name]
	at := -1
	for range nth + 1 {
		i := strings.Index(text[at+1:], sub)
		if i < 0 {
			p.t.Fatalf("%q occurs fewer than %d times in %s", sub, nth+1, name)
		}
		at += i + 1
	}
	line := strings.Count(text[:at], "\n")
	return line, at - (strings.LastIndexByte(text[:at], '\n') + 1)
}

func (p *nativeProject) params(name, sub string, nth int, extra string) string {
	line, character := p.pos(name, sub, nth)
	return positionParams(p.uri(name), line, character+1, extra)
}

func (p *nativeProject) at(name, sub string, nth int) lsp.Location {
	line, character := p.pos(name, sub, nth)
	return lsp.Location{URI: p.uri(name), Range: lsp.Range{
		Start: lsp.Position{Line: uint32(line), Character: uint32(character)},
		End:   lsp.Position{Line: uint32(line), Character: uint32(character + len(sub))},
	}}
}

const nativeCapabilities = `"capabilities": {"textDocument": {"diagnostic": {"relatedDocumentSupport": false}, "publishDiagnostics": {"codeDescriptionSupport": true}, "documentSymbol": {"hierarchicalDocumentSymbolSupport": true}}, "workspace": {"workspaceEdit": {"documentChanges": true}}}`

var justProject = map[string]string{
	"justfile":       "import 'ci.just'\nmod tools\n\nexport TOKEN := \"secret\"\n\n# Build the project\nbuild target=\"debug\": (compile target)\n    echo {{target}} $TOKEN\n    just release\n\ncompile profile:\n    cargo build --profile {{profile}}\n",
	"ci.just":        "release: build tools::lint\n    echo releasing {{version}}\n\nversion := \"1.0\"\n",
	"tools/mod.just": "lint:\n    shellcheck *.sh\n",
}

func TestJustfileNavigationAcrossFilesAndShell(t *testing.T) {
	t.Parallel()

	p := openNativeProject(t, nativeCapabilities, justProject, "justfile")
	definition := func(name, sub string, nth int) []lsp.Location {
		return decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params(name, sub, nth, "")))
	}
	if got := definition("justfile", "release", 0); len(got) != 1 || got[0] != p.at("ci.just", "release", 0) {
		t.Errorf("definition of just release in the shell = %+v, want the recipe in ci.just", got)
	}
	if got := definition("justfile", "TOKEN", 1); len(got) != 1 || got[0] != p.at("justfile", "TOKEN", 0) {
		t.Errorf("definition of $TOKEN = %+v", got)
	}
	if got := definition("justfile", "compile", 0); len(got) != 1 || got[0] != p.at("justfile", "compile", 1) {
		t.Errorf("definition of the compile dependency = %+v", got)
	}
	if got := definition("justfile", "ci.just", 0); len(got) != 1 || got[0].URI != p.uri("ci.just") {
		t.Errorf("definition of the import path = %+v", got)
	}
	refs := decodeResult[[]lsp.Location](t, p.c.call("textDocument/references", p.params("justfile", "build", 0, `, "context": {"includeDeclaration": true}`)))
	want := []lsp.Location{p.at("ci.just", "build", 0), p.at("justfile", "build", 0)}
	if !slices.Equal(refs, want) {
		t.Errorf("references of build = %+v, want %+v", refs, want)
	}
	highlights := decodeResult[[]lsp.DocumentHighlight](t, p.c.call("textDocument/documentHighlight", p.params("justfile", "target", 1, "")))
	if len(highlights) != 3 || highlights[0].Kind != lsp.HighlightWrite {
		t.Errorf("highlights of target = %+v", highlights)
	}
}

func TestJustfileRenameReachesTheShell(t *testing.T) {
	t.Parallel()

	p := openNativeProject(t, nativeCapabilities, justProject, "justfile")
	prepared := decodeResult[lsp.PrepareRenameResult](t, p.c.call("textDocument/prepareRename", p.params("justfile", "TOKEN", 0, "")))
	if prepared.Placeholder != "TOKEN" || prepared.Range != p.at("justfile", "TOKEN", 0).Range {
		t.Errorf("prepareRename = %+v", prepared)
	}
	edit := decodeResult[lsp.WorkspaceEdit](t, p.c.call("textDocument/rename", p.params("justfile", "TOKEN", 0, `, "newName": "API_TOKEN"`)))
	var ranges []lsp.Range
	for _, change := range edit.DocumentChanges {
		for _, e := range change.Edits {
			if e.NewText != "API_TOKEN" {
				t.Errorf("edit %+v", e)
			}
			ranges = append(ranges, e.Range)
		}
	}
	if want := []lsp.Range{p.at("justfile", "TOKEN", 0).Range, p.at("justfile", "TOKEN", 1).Range}; !slices.Equal(ranges, want) {
		t.Errorf("rename edits %+v, want %+v", ranges, want)
	}
	bad := p.c.call("textDocument/rename", p.params("justfile", "TOKEN", 0, `, "newName": "api-token"`))
	if bad.Error == nil || !strings.Contains(bad.Error.Message, "not a valid") {
		t.Errorf("renaming an exported variable to a name the shell cannot read = %+v", bad)
	}
	edit = decodeResult[lsp.WorkspaceEdit](t, p.c.call("textDocument/rename", p.params("justfile", "release", 0, `, "newName": "ship"`)))
	uris := map[string]int{}
	for _, change := range edit.DocumentChanges {
		uris[change.TextDocument.URI] += len(change.Edits)
	}
	if uris[p.uri("justfile")] != 1 || uris[p.uri("ci.just")] != 1 {
		t.Errorf("renaming release from just release edits %+v", uris)
	}
}

func TestJustfileHoverCompletionAndStructure(t *testing.T) {
	t.Parallel()

	p := openNativeProject(t, nativeCapabilities, justProject, "justfile")
	hover := decodeResult[*lsp.Hover](t, p.c.call("textDocument/hover", p.params("justfile", "build", 0, "")))
	if hover == nil || !strings.Contains(hover.Contents.Value, "Build the project") || !strings.Contains(hover.Contents.Value, `build target="debug"`) {
		t.Errorf("hover on build = %+v", hover)
	}
	hover = decodeResult[*lsp.Hover](t, p.c.call("textDocument/hover", p.params("justfile", "TOKEN", 1, "")))
	if hover == nil || !strings.Contains(hover.Contents.Value, "exported to the shell") {
		t.Errorf("hover on $TOKEN = %+v", hover)
	}
	labels := completionLabels(decodeResult[lsp.CompletionList](t, p.c.call("textDocument/completion", p.params("justfile", "target}}", 0, ""))))
	for _, want := range []string{"target", "TOKEN", "version", "uppercase"} {
		if !slices.Contains(labels, want) {
			t.Errorf("completion in {{ }} lacks %s: %v", want, labels)
		}
	}
	labels = completionLabels(decodeResult[lsp.CompletionList](t, p.c.call("textDocument/completion", p.params("justfile", "compile target", 0, ""))))
	for _, want := range []string{"build", "compile", "release", "tools"} {
		if !slices.Contains(labels, want) {
			t.Errorf("completion in the dependencies lacks %s: %v", want, labels)
		}
	}
	symbols := decodeResult[[]lsp.DocumentSymbol](t, p.c.call("textDocument/documentSymbol", `{"textDocument": {"uri": "`+p.uri("justfile")+`"}}`))
	var names []string
	for _, s := range symbols {
		names = append(names, s.Name)
		if s.Name == "build" && (len(s.Children) != 1 || s.Children[0].Name != "target") {
			t.Errorf("build's children = %+v", s.Children)
		}
	}
	if want := []string{"tools", "TOKEN", "build", "compile"}; !slices.Equal(names, want) {
		t.Errorf("document symbols = %v, want %v", names, want)
	}
	folds := decodeResult[[]lsp.FoldingRange](t, p.c.call("textDocument/foldingRange", `{"textDocument": {"uri": "`+p.uri("justfile")+`"}}`))
	if !slices.ContainsFunc(folds, func(f lsp.FoldingRange) bool { return f.StartLine == 6 && f.EndLine == 8 }) {
		t.Errorf("folds = %+v, want the build recipe", folds)
	}
	links := decodeResult[[]lsp.DocumentLink](t, p.c.call("textDocument/documentLink", `{"textDocument": {"uri": "`+p.uri("justfile")+`"}}`))
	var targets []string
	for _, l := range links {
		targets = append(targets, l.Target)
	}
	if want := []string{p.uri("ci.just"), p.uri("tools/mod.just")}; !slices.Equal(targets, want) {
		t.Errorf("links = %v, want %v", targets, want)
	}
	workspace := decodeResult[[]lsp.SymbolInformation](t, p.c.call("workspace/symbol", `{"query": "lint"}`))
	if !slices.ContainsFunc(workspace, func(s lsp.SymbolInformation) bool {
		return s.Name == "lint" && s.Location.URI == p.uri("tools/mod.just")
	}) {
		t.Errorf("workspace symbols for lint = %+v", workspace)
	}
}

func TestJustfileDiagnosticsNameNativeChecks(t *testing.T) {
	t.Parallel()

	files := map[string]string{"justfile": "build: compile\n    echo {{nope}} $1\n"}
	p := openNativeProject(t, nativeCapabilities, files, "justfile")
	items := pullDiagnostics(t, p.c, p.uri("justfile"))
	if got, want := codes(items), []string{"SC2086", "SL5002", "SL5003"}; !slices.Equal(got, want) {
		t.Fatalf("diagnostics = %v, want %v", got, want)
	}
	for _, d := range items {
		if strings.HasPrefix(d.Code, "SL5") && (d.CodeDescription == nil || !strings.Contains(d.CodeDescription.Href, "rules.md#"+strings.ToLower(d.Code))) {
			t.Errorf("%s links to %+v", d.Code, d.CodeDescription)
		}
	}
	if items[1].Range != p.at("justfile", "compile", 0).Range || items[2].Range != p.at("justfile", "nope", 0).Range {
		t.Errorf("ranges = %+v", items)
	}
}

func TestJustfileFormatting(t *testing.T) {
	t.Parallel()

	files := map[string]string{"justfile": "set   quiet\nbuild:\n  echo {{  x  }}\nx:='1'\n"}
	p := openNativeProject(t, nativeCapabilities, files, "justfile")
	edits := decodeResult[[]lsp.TextEdit](t, p.c.call("textDocument/formatting", `{"textDocument": {"uri": "`+p.uri("justfile")+`"}, "options": {"tabSize": 4, "insertSpaces": true}}`))
	text := applyTextEdits(t, files["justfile"], edits)
	if want := "set quiet\nbuild:\n    echo {{ x }}\nx := '1'\n"; text != want {
		t.Errorf("formatted justfile = %q, want %q", text, want)
	}
	broken := map[string]string{"justfile": "build: # explains build\n    echo\n"}
	q := openNativeProject(t, nativeCapabilities, broken, "justfile")
	if msg := q.c.call("textDocument/formatting", `{"textDocument": {"uri": "`+q.uri("justfile")+`"}, "options": {"tabSize": 4, "insertSpaces": true}}`); msg.Error == nil || !strings.Contains(msg.Error.Message, "would drop the comment") {
		t.Errorf("formatting a justfile whose comment just --fmt drops = %+v", msg)
	}
}

func applyTextEdits(t *testing.T, text string, edits []lsp.TextEdit) string {
	t.Helper()
	lines := strings.SplitAfter(text, "\n")
	offset := func(p lsp.Position) int {
		n := 0
		for _, l := range lines[:p.Line] {
			n += len(l)
		}
		return n + int(p.Character)
	}
	slices.SortFunc(edits, func(a, b lsp.TextEdit) int { return offset(b.Range.Start) - offset(a.Range.Start) })
	for _, e := range edits {
		text = text[:offset(e.Range.Start)] + e.NewText + text[offset(e.Range.End):]
	}
	return text
}

func TestRecipeRunLenses(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"justfile": "greet:\n    @echo hello from just\n\nwith-arg name:\n    echo {{name}}\n",
		"Makefile": "hello:\n\t@echo hello from make\n\n%.o: %.c\n\tcc -c $<\n",
	}
	p := openNativeProject(t, nativeCapabilities, files, "justfile", "Makefile")
	for _, tt := range []struct {
		file, target, tool, output string
	}{
		{"justfile", "greet", "just", "hello from just"},
		{"Makefile", "hello", "make", "hello from make"},
	} {
		lenses := decodeResult[[]lsp.CodeLens](t, p.c.call("textDocument/codeLens", `{"textDocument": {"uri": "`+p.uri(tt.file)+`"}}`))
		var runs []lsp.CodeLens
		for _, l := range lenses {
			if l.Command != nil && l.Command.Command == commandRunRecipe {
				runs = append(runs, l)
			}
		}
		if len(runs) != 1 || runs[0].Range != p.at(tt.file, tt.target, 0).Range {
			t.Errorf("%s run lenses = %+v, want one on %s", tt.file, runs, tt.target)
			continue
		}
		if _, err := exec.LookPath(tt.tool); err != nil {
			continue
		}
		args, err := json.Marshal(runs[0].Command.Arguments)
		if err != nil {
			t.Fatal(err)
		}
		result := decodeResult[recipeRunResult](t, p.c.call("workspace/executeCommand", `{"command": "`+commandRunRecipe+`", "arguments": `+string(args)+`}`))
		if result.ExitCode != 0 || !strings.Contains(result.Output, tt.output) {
			t.Errorf("running %s = %+v", tt.target, result)
		}
	}
	bad := p.c.call("workspace/executeCommand", `{"command": "`+commandRunRecipe+`", "arguments": [{"uri": "`+p.uri("justfile")+`", "name": "with-arg"}]}`)
	if bad.Error == nil || !strings.Contains(bad.Error.Message, `"with-arg" takes 1 argument, not 0`) {
		t.Errorf("running a recipe that needs an argument = %+v", bad)
	}
	if _, err := exec.LookPath("just"); err == nil {
		result := decodeResult[recipeRunResult](t, p.c.call("workspace/executeCommand", `{"command": "`+commandRunRecipe+`", "arguments": [{"uri": "`+p.uri("justfile")+`", "name": "with-arg", "arguments": ["world"]}]}`))
		if result.ExitCode != 0 || !strings.Contains(result.Output, "world") {
			t.Errorf("running with-arg world = %+v", result)
		}
	}
}

func TestRecipeRunLensesPromptForArguments(t *testing.T) {
	t.Parallel()

	files := map[string]string{"justfile": "greet:\n    echo hi\n\nwith-arg name level=\"1\" *rest:\n    echo {{name}}\n"}
	caps := `"capabilities": {"experimental": {"shellLsp": {"promptsForArguments": true}}}`
	p := openNativeProject(t, caps, files, "justfile")
	lenses := decodeResult[[]lsp.CodeLens](t, p.c.call("textDocument/codeLens", `{"textDocument": {"uri": "`+p.uri("justfile")+`"}}`))
	var titles []string
	var prompted recipeRunArgs
	for _, l := range lenses {
		if l.Command == nil || l.Command.Command != commandRunRecipe {
			continue
		}
		titles = append(titles, l.Command.Title)
		if l.Command.Title == "▶ Run…" {
			if err := json.Unmarshal(l.Command.Arguments[0], &prompted); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !slices.Equal(titles, []string{"▶ Run", "▶ Run…"}) {
		t.Fatalf("run lens titles = %q", titles)
	}
	want := []model.RunParam{{Name: "name"}, {Name: "level", Default: `"1"`, Optional: true}, {Name: "rest", Optional: true, Variadic: true}}
	if prompted.Name != "with-arg" || !slices.Equal(prompted.Parameters, want) {
		t.Errorf("prompted lens arguments = %+v", prompted)
	}
	listed := p.c.call("workspace/executeCommand", `{"command": "`+commandListRecipes+`"}`)
	if strings.Contains(string(listed.Result), `"parameters":null`) {
		t.Errorf("a recipe without parameters lists them as null instead of an empty list: %s", listed.Result)
	}
	listing := decodeResult[[]recipeListing](t, listed)
	if len(listing) != 2 || listing[1].Name != "with-arg" || listing[1].Tool != "just" || listing[1].Argv[len(listing[1].Argv)-1] != "with-arg" || !slices.Equal(listing[1].Parameters, want) {
		t.Errorf("listed recipes = %+v", listing)
	}
}

func TestMakefileNavigationAcrossIncludes(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"Makefile":  "include common.mk\nexport TOKEN := x\n\nall: build\n\t$(MAKE) clean\n\t@echo $(CFLAGS) $$TOKEN\n",
		"common.mk": "CFLAGS := -O2\n\nbuild:\n\t@echo build\n\nclean:\n\trm -f out\n",
	}
	p := openNativeProject(t, nativeCapabilities, files, "Makefile")
	definition := func(name, sub string, nth int) []lsp.Location {
		return decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params(name, sub, nth, "")))
	}
	for _, tt := range []struct {
		sub  string
		nth  int
		want lsp.Location
	}{
		{"build", 0, p.at("common.mk", "build", 0)},
		{"clean", 0, p.at("common.mk", "clean", 0)},
		{"CFLAGS", 0, p.at("common.mk", "CFLAGS", 0)},
		{"TOKEN", 1, p.at("Makefile", "TOKEN", 0)},
	} {
		if got := definition("Makefile", tt.sub, tt.nth); len(got) != 1 || got[0] != tt.want {
			t.Errorf("definition of %s = %+v, want %+v", tt.sub, got, tt.want)
		}
	}
	if items := pullDiagnostics(t, p.c, p.uri("Makefile")); len(items) != 0 {
		t.Errorf("diagnostics = %+v", items)
	}
	hover := decodeResult[*lsp.Hover](t, p.c.call("textDocument/hover", p.params("Makefile", "MAKE", 0, "")))
	if hover == nil || !strings.Contains(hover.Contents.Value, "recursive make") {
		t.Errorf("hover on $(MAKE) = %+v", hover)
	}
}

func TestJustfileTokensLensesAndRangeFormatting(t *testing.T) {
	t.Parallel()

	files := map[string]string{"justfile": "set quiet\n[private]\nbuild target:\n    echo {{target}} $HOME\n\nci: (build \"x\")\n    echo   done\n"}
	p := openNativeProject(t, nativeCapabilities, files, "justfile")
	tokens := decodeTokens(t, decodeResult[lsp.SemanticTokens](t, p.c.call("textDocument/semanticTokens/full", `{"textDocument": {"uri": "`+p.uri("justfile")+`"}}`)).Data)
	token := func(sub string, nth int, kind string) placed {
		l := p.at("justfile", sub, nth)
		return placed{line: l.Range.Start.Line, start: l.Range.Start.Character, length: l.Range.End.Character - l.Range.Start.Character, kind: kind}
	}
	for _, want := range []placed{
		token("set", 0, "keyword"),
		token("quiet", 0, "property"),
		token("private", 0, "decorator"),
		token("build", 0, "function"),
		token("target", 0, "parameter"),
		token("echo", 0, "function"),
		token("target", 1, "variable"),
		token("HOME", 0, "variable"),
		token("build", 1, "function"),
	} {
		if !slices.Contains(tokens, want) {
			t.Errorf("no %+v among %+v", want, tokens)
		}
	}
	lenses := decodeResult[[]lsp.CodeLens](t, p.c.call("textDocument/codeLens", `{"textDocument": {"uri": "`+p.uri("justfile")+`"}}`))
	i := slices.IndexFunc(lenses, func(l lsp.CodeLens) bool { return l.Command == nil && l.Range == p.at("justfile", "build", 0).Range })
	if i < 0 {
		t.Fatalf("no reference lens on build among %+v", lenses)
	}
	encoded, err := json.Marshal(lenses[i])
	if err != nil {
		t.Fatal(err)
	}
	if resolved := decodeResult[lsp.CodeLens](t, p.c.call("codeLens/resolve", string(encoded))); resolved.Command == nil || resolved.Command.Title != "1 reference" {
		t.Errorf("resolved lens = %+v", resolved)
	}
	if !slices.ContainsFunc(lenses, func(l lsp.CodeLens) bool { return l.Command != nil && l.Range == p.at("justfile", "ci", 0).Range }) || slices.ContainsFunc(lenses, func(l lsp.CodeLens) bool { return l.Command != nil && l.Range == p.at("justfile", "build", 0).Range }) {
		t.Errorf("run lenses: want one on ci and none on build, which needs an argument: %+v", lenses)
	}
	if msg := p.c.call("textDocument/prepareRename", p.params("justfile", "quiet", 0, "")); msg.Error == nil {
		t.Errorf("prepareRename on a setting = %s, want an error", msg.Result)
	}
	edits := decodeResult[[]lsp.TextEdit](t, p.c.call("textDocument/rangeFormatting", `{"textDocument": {"uri": "`+p.uri("justfile")+`"}, "range": {"start": {"line": 6, "character": 0}, "end": {"line": 6, "character": 15}}, "options": {"tabSize": 4, "insertSpaces": true}}`))
	for _, e := range edits {
		if e.Range.Start.Line != 6 {
			t.Errorf("range formatting edits line %d: %+v", e.Range.Start.Line, e)
		}
	}
	report := decodeResult[lsp.WorkspaceDiagnosticReport](t, p.c.call("workspace/diagnostic", `{"previousResultIds": []}`))
	if len(report.Items) != 1 || report.Items[0].URI != p.uri("justfile") {
		t.Errorf("workspace diagnostics = %+v", report.Items)
	}
}

func TestEditingAnImportedFileRediagnosesTheImporter(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"justfile": "import 'ci.just'\nbuild: release\n    echo built\n",
		"ci.just":  "release:\n    echo released\n",
	}
	p := openNativeProject(t, nativeCapabilities, files, "justfile", "ci.just")
	report := func() lsp.FullDocumentDiagnosticReport {
		return decodeResult[lsp.FullDocumentDiagnosticReport](t, p.c.call("textDocument/diagnostic", `{"textDocument": {"uri": "`+p.uri("justfile")+`"}}`))
	}
	before := report()
	if len(before.Items) > 0 {
		t.Fatalf("diagnostics before the edit = %+v", before.Items)
	}
	p.c.notify("textDocument/didChange", `{"textDocument": {"uri": "`+p.uri("ci.just")+`", "version": 2}, "contentChanges": [{"text": "publish:\n    echo published\n"}]}`)
	after := report()
	if after.ResultID == before.ResultID {
		t.Errorf("the result id %q did not change after the imported file did", after.ResultID)
	}
	if got := codes(after.Items); !slices.Equal(got, []string{"SL5002"}) {
		t.Errorf("diagnostics after the edit = %v, want the unknown recipe", got)
	}
}

func TestEditingAnImportedFileRepublishesTheImporter(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"justfile": "import 'ci.just'\nbuild: release\n    echo built\n",
		"ci.just":  "release:\n    echo released\n",
	}
	p := openNativeProject(t, `"capabilities": {}`, files, "justfile", "ci.just")
	unknown := func(params json.RawMessage) bool {
		var published lsp.PublishDiagnosticsParams
		return json.Unmarshal(params, &published) == nil && published.URI == p.uri("justfile") && slices.Equal(codes(published.Diagnostics), []string{"SL5002"})
	}
	p.c.notify("textDocument/didChange", `{"textDocument": {"uri": "`+p.uri("ci.just")+`", "version": 2}, "contentChanges": [{"text": "publish:\n    echo published\n"}]}`)
	p.c.awaitNotification("textDocument/publishDiagnostics", unknown)
}

func TestJustfileCallHierarchy(t *testing.T) {
	t.Parallel()

	files := map[string]string{"justfile": "build: compile\n    just test\n\ncompile:\n    cc main.c\n\ntest:\n    ./run\n"}
	p := openNativeProject(t, nativeCapabilities, files, "justfile")
	items := decodeResult[[]lsp.CallHierarchyItem](t, p.c.call("textDocument/prepareCallHierarchy", p.params("justfile", "build", 0, "")))
	if len(items) != 1 || items[0].Name != "build" || items[0].SelectionRange != p.at("justfile", "build", 0).Range {
		t.Fatalf("prepared items = %+v", items)
	}
	item, err := json.Marshal(items[0])
	if err != nil {
		t.Fatal(err)
	}
	outgoing := decodeResult[[]lsp.CallHierarchyOutgoingCall](t, p.c.call("callHierarchy/outgoingCalls", `{"item": `+string(item)+`}`))
	var names []string
	for _, c := range outgoing {
		names = append(names, c.To.Name)
	}
	if !slices.Equal(names, []string{"compile", "test"}) || outgoing[1].FromRanges[0] != p.at("justfile", "test", 0).Range {
		t.Errorf("outgoing calls = %+v", outgoing)
	}
	callee, err := json.Marshal(outgoing[0].To)
	if err != nil {
		t.Fatal(err)
	}
	incoming := decodeResult[[]lsp.CallHierarchyIncomingCall](t, p.c.call("callHierarchy/incomingCalls", `{"item": `+string(callee)+`}`))
	if len(incoming) != 1 || incoming[0].From.Name != "build" || incoming[0].FromRanges[0] != p.at("justfile", "compile", 0).Range {
		t.Errorf("incoming calls of compile = %+v", incoming)
	}
}

func TestNativeSignatureHelp(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"justfile": "x := replace(\"a\", \"b\", \"c\")\nbuild target profile=\"dev\":\n    echo {{target}} {{profile}}\nci: (build \"x\" \"y\")\n    just build debug \n",
		"Makefile": "x := $(subst a,b,c)\n",
	}
	p := openNativeProject(t, nativeCapabilities, files, "justfile", "Makefile")
	help := func(name, sub string, nth int) lsp.SignatureHelp {
		t.Helper()
		return decodeResult[lsp.SignatureHelp](t, p.c.call("textDocument/signatureHelp", p.params(name, sub, nth, "")))
	}
	for _, tt := range []struct {
		name, file, sub string
		nth             int
		label, param    string
		active          uint32
	}{
		{"builtin function", "justfile", `"b"`, 0, "replace(s, from, to)", "from", 1},
		{"dependency", "justfile", `"y"`, 0, `build target profile="dev"`, `profile="dev"`, 1},
		{"recipe run from the shell", "justfile", "debug", 0, `build target profile="dev"`, "target", 0},
		{"make function", "Makefile", ",c", 0, "$(subst from,to,text)", "text", 2},
	} {
		got := help(tt.file, tt.sub, tt.nth)
		if len(got.Signatures) != 1 || got.Signatures[0].Label != tt.label || got.ActiveParameter == nil || *got.ActiveParameter != tt.active {
			t.Errorf("%s: signature help = %+v", tt.name, got)
			continue
		}
		label := got.Signatures[0].Parameters[tt.active].Label
		if part := tt.label[label[0]:label[1]]; part != tt.param {
			t.Errorf("%s: active parameter %q, want %q", tt.name, part, tt.param)
		}
	}
}

func TestNativeInlayHints(t *testing.T) {
	t.Parallel()

	files := map[string]string{"justfile": "version := \"1.0\"\nbuild target:\n    echo {{version}} {{target}}\nci: (build \"x\")\n    just build debug\n"}
	p := openNativeProject(t, nativeCapabilities, files, "justfile")
	hints := decodeResult[[]lsp.InlayHint](t, p.c.call("textDocument/inlayHint", `{"textDocument": {"uri": "`+p.uri("justfile")+`"}, "range": {"start": {"line": 0, "character": 0}, "end": {"line": 9, "character": 0}}}`))
	var got []string
	for _, h := range hints {
		got = append(got, fmt.Sprintf("%d:%d %v", h.Position.Line, h.Position.Character, h.Label))
	}
	want := []string{`2:18 = "1.0"`, "3:11 target:", "4:15 target:"}
	if !slices.Equal(got, want) {
		t.Errorf("hints = %q, want %q", got, want)
	}
}

func TestNativeQuickFixes(t *testing.T) {
	t.Parallel()

	files := map[string]string{"justfile": "build: compil\n    echo\ncompile:\n    cc\n"}
	caps := `"capabilities": {"textDocument": {"diagnostic": {}, "codeAction": {"codeActionLiteralSupport": {"codeActionKind": {"valueSet": ["quickfix"]}}}}, "workspace": {"workspaceEdit": {"documentChanges": true, "resourceOperations": ["create"]}}}`
	p := openNativeProject(t, caps, files, "justfile")
	diags := pullDiagnostics(t, p.c, p.uri("justfile"))
	raw, err := json.Marshal(diags)
	if err != nil {
		t.Fatal(err)
	}
	r := p.at("justfile", "compil", 0).Range
	params := fmt.Sprintf(`{"textDocument": {"uri": %q}, "range": {"start": {"line": %d, "character": %d}, "end": {"line": %d, "character": %d}}, "context": {"diagnostics": %s}}`, p.uri("justfile"), r.Start.Line, r.Start.Character, r.End.Line, r.End.Character, raw)
	actions := decodeResult[[]lsp.CodeAction](t, p.c.call("textDocument/codeAction", params))
	var titles []string
	for _, a := range actions {
		titles = append(titles, a.Title)
	}
	if !slices.Contains(titles, "Change to `compile`") || !slices.Contains(titles, "Add recipe `compil`") {
		t.Fatalf("actions = %q", titles)
	}
	change := actions[slices.Index(titles, "Change to `compile`")]
	if !change.IsPreferred || len(change.Diagnostics) != 1 || change.Diagnostics[0].Code != "SL5002" || change.Edit == nil || len(change.Edit.DocumentChanges) != 1 || change.Edit.DocumentChanges[0].Edits[0].NewText != "compile" {
		t.Errorf("change action = %+v", change)
	}
}

func TestNativeSelectionRanges(t *testing.T) {
	t.Parallel()

	files := map[string]string{"justfile": "x := uppercase(\"a\" + y)\ny := \"b\"\n\n[private]\nbuild:\n    echo $HOME done\n"}
	p := openNativeProject(t, nativeCapabilities, files, "justfile")
	line, character := p.pos("justfile", "y)", 0)
	home, homeAt := p.pos("justfile", "HOME", 0)
	ranges := decodeResult[[]lsp.SelectionRange](t, p.c.call("textDocument/selectionRange", fmt.Sprintf(`{"textDocument": {"uri": %q}, "positions": [{"line": %d, "character": %d}, {"line": %d, "character": %d}]}`, p.uri("justfile"), line, character, home, homeAt+1)))
	text := files["justfile"]
	chain := func(r *lsp.SelectionRange) []string {
		var out []string
		for ; r != nil; r = r.Parent {
			start := offsetOf(text, r.Range.Start)
			out = append(out, text[start:offsetOf(text, r.Range.End)])
		}
		return out
	}
	if got, want := chain(&ranges[0]), []string{"y", `"a" + y`, `uppercase("a" + y)`, "x := uppercase(\"a\" + y)", text}; !slices.Equal(got, want) {
		t.Errorf("expression selection = %q, want %q", got, want)
	}
	if got, want := chain(&ranges[1]), []string{"HOME", "$HOME", "echo $HOME done", "    echo $HOME done", "build:\n    echo $HOME done", "[private]\nbuild:\n    echo $HOME done", text}; !slices.Equal(got, want) {
		t.Errorf("shell selection = %q, want %q", got, want)
	}
}

func offsetOf(text string, pos lsp.Position) int {
	offset := 0
	for range pos.Line {
		offset += strings.IndexByte(text[offset:], '\n') + 1
	}
	return offset + int(pos.Character)
}

func TestDialectOfTheRecipeUnderTheCursor(t *testing.T) {
	t.Parallel()

	files := map[string]string{"justfile": "plain:\n    echo plain\nbash:\n    #!/usr/bin/env bash\n    echo bash\n"}
	p := openNativeProject(t, nativeCapabilities, files, "justfile")
	dialectAt := func(params string) string {
		var got struct {
			Dialect string `json:"dialect"`
		}
		if err := json.Unmarshal(p.c.call("shellLsp/documentDialect", params).Result, &got); err != nil {
			t.Fatal(err)
		}
		return got.Dialect
	}
	if got := dialectAt(p.params("justfile", "plain", 1, "")); got != "sh" {
		t.Errorf("dialect in the plain recipe = %q, want sh", got)
	}
	if got := dialectAt(p.params("justfile", "bash", 2, "")); got != "bash" {
		t.Errorf("dialect in the bash recipe = %q, want bash", got)
	}
	if got := dialectAt(`{"textDocument": {"uri": "` + p.uri("justfile") + `"}}`); got != "bash" {
		t.Errorf("dialect of the whole justfile = %q, want bash, the widest of its shells", got)
	}
}

func TestNixShellReachesTheDerivationAttributesItReads(t *testing.T) {
	t.Parallel()

	files := map[string]string{"default.nix": "stdenv.mkDerivation {\n  pname = \"hello\";\n  version = \"1.0\";\n  buildPhase = ''\n    make PREFIX=$out NAME=$pname\n  '';\n  installPhase = ''\n    install -D hello $out/bin/''${pname}-$version\n  '';\n}\n"}
	p := openNativeProject(t, nativeCapabilities, files, "default.nix")
	definition := decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params("default.nix", "pname", 1, "")))
	if want := []lsp.Location{p.at("default.nix", "pname", 0)}; !slices.Equal(definition, want) {
		t.Errorf("definition of $pname = %+v, want %+v", definition, want)
	}
	refs := decodeResult[[]lsp.Location](t, p.c.call("textDocument/references", p.params("default.nix", "pname", 2, `, "context": {"includeDeclaration": true}`)))
	if want := []lsp.Location{p.at("default.nix", "pname", 0), p.at("default.nix", "pname", 1), p.at("default.nix", "pname", 2)}; !slices.Equal(refs, want) {
		t.Errorf("references of ${pname} = %+v, want %+v", refs, want)
	}
	hover := decodeResult[*lsp.Hover](t, p.c.call("textDocument/hover", p.params("default.nix", "version", 1, "")))
	if hover == nil || !strings.Contains(hover.Contents.Value, "```nix\nversion = \"1.0\";\n```") || !strings.Contains(hover.Contents.Value, "Set in the environment by the derivation.") || !strings.Contains(hover.Contents.Value, "exported to the shell") {
		t.Errorf("hover on $version = %+v", hover)
	}
	rename := p.c.call("textDocument/rename", p.params("default.nix", "pname", 1, `, "newName": "name"`))
	if rename.Error == nil || !strings.Contains(rename.Error.Message, "Nix file") {
		t.Errorf("renaming a derivation attribute from the shell = %+v", rename)
	}
	if binding := decodeResult[*lsp.Hover](t, p.c.call("textDocument/hover", p.params("default.nix", "pname", 0, ""))); binding == nil || !strings.Contains(binding.Contents.Value, "pname = \"hello\";") {
		t.Errorf("hover on the Nix binding = %+v", binding)
	}
	fromBinding := decodeResult[[]lsp.Location](t, p.c.call("textDocument/references", p.params("default.nix", "pname", 0, `, "context": {"includeDeclaration": true}`)))
	if !slices.Equal(fromBinding, refs) {
		t.Errorf("references from the Nix binding = %+v, want those from the shell", fromBinding)
	}
	refused := p.c.call("textDocument/rename", p.params("default.nix", "pname", 0, `, "newName": "name"`))
	if refused.Error == nil || !strings.Contains(refused.Error.Message, "Nix file") {
		t.Errorf("renaming an attribute the shell reads from the Nix side = %+v", refused)
	}
	symbols := decodeResult[[]lsp.DocumentSymbol](t, p.c.call("textDocument/documentSymbol", `{"textDocument": {"uri": "`+p.uri("default.nix")+`"}}`))
	pnames := 0
	for _, s := range symbols {
		if s.Name == "pname" {
			pnames++
		}
	}
	if pnames != 1 {
		t.Errorf("document symbols list pname %d times: %+v", pnames, symbols)
	}
}

func corpusFile(t *testing.T, parts ...string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(append([]string{"..", "..", "corpus", "nix"}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func symbolNames(symbols []lsp.DocumentSymbol) []string {
	out := make([]string, len(symbols))
	for i, s := range symbols {
		out[i] = s.Name
	}
	return out
}

func childSymbol(t *testing.T, symbols []lsp.DocumentSymbol, name string) lsp.DocumentSymbol {
	t.Helper()
	i := slices.IndexFunc(symbols, func(s lsp.DocumentSymbol) bool { return s.Name == name })
	if i < 0 {
		t.Fatalf("no symbol %s in %q", name, symbolNames(symbols))
	}
	return symbols[i]
}

func editTexts(edit lsp.WorkspaceEdit) []string {
	var out []string
	for _, change := range edit.DocumentChanges {
		for _, e := range change.Edits {
			out = append(out, e.NewText)
		}
	}
	return out
}

func TestNixSingleFlakeAnswersOutsideItsShell(t *testing.T) {
	t.Parallel()

	const name = "flake.nix"
	text := corpusFile(t, "single-flake", name)
	p := openNativeProject(t, nativeCapabilities, map[string]string{name: text}, name)

	if got, want := decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params(name, "revision", 1, ""))), []lsp.Location{p.at(name, "revision", 0)}; !slices.Equal(got, want) {
		t.Errorf("definition of ${revision} = %+v, want %+v", got, want)
	}
	refs := decodeResult[[]lsp.Location](t, p.c.call("textDocument/references", p.params(name, "pkgsFor", 0, `, "context": {"includeDeclaration": true}`)))
	if want := strings.Count(text, "pkgsFor"); len(refs) != want {
		t.Errorf("references of pkgsFor = %d, want %d: %+v", len(refs), want, refs)
	}
	highlights := decodeResult[[]lsp.DocumentHighlight](t, p.c.call("textDocument/documentHighlight", p.params(name, "pkgsFor", 1, "")))
	if len(highlights) != len(refs) || highlights[0].Kind != lsp.HighlightWrite {
		t.Errorf("highlights of pkgsFor = %+v", highlights)
	}
	alias := decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params(name, "inputs.nixpkgs.shortRev", 0, "")))
	if want := []lsp.Location{p.at(name, "inputs", 1)}; !slices.Equal(alias, want) {
		t.Errorf("definition of the alias = %+v, want %+v", alias, want)
	}
	hover := decodeResult[*lsp.Hover](t, p.c.call("textDocument/hover", p.params(name, "revision", 1, "")))
	if hover == nil || !strings.Contains(hover.Contents.Value, `revision = inputs.nixpkgs.shortRev or "dirty";`) {
		t.Errorf("hover on ${revision} = %+v", hover)
	}
	shell := decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params(name, "LOGSHIP_SPOOL", 1, "")))
	if want := []lsp.Location{p.at(name, "LOGSHIP_SPOOL", 0)}; !slices.Equal(shell, want) {
		t.Errorf("definition of $LOGSHIP_SPOOL = %+v, want %+v", shell, want)
	}

	symbols := decodeResult[[]lsp.DocumentSymbol](t, p.c.call("textDocument/documentSymbol", `{"textDocument": {"uri": "`+p.uri(name)+`"}}`))
	for _, want := range []string{"description", "inputs", "outputs"} {
		childSymbol(t, symbols, want)
	}
	outputs := childSymbol(t, symbols, "outputs")
	if outputs.Kind != lsp.SymbolFunction {
		t.Errorf("outputs has kind %v, want a function", outputs.Kind)
	}
	block := childSymbol(t, outputs.Children, "let")
	for _, want := range []string{"systems", "forAllSystems", "revision", "pkgsFor"} {
		childSymbol(t, block.Children, want)
	}
	if fn := childSymbol(t, block.Children, "pkgsFor"); fn.Kind != lsp.SymbolFunction || len(fn.Children) == 0 || fn.Children[0].Name != "system" {
		t.Errorf("pkgsFor = %+v, want a function with its argument", fn)
	}
	packages := childSymbol(t, outputs.Children, "packages")
	for _, want := range []string{"system", "let", "default", "logship", "manual"} {
		childSymbol(t, packages.Children, want)
	}
	workspace := decodeResult[[]lsp.SymbolInformation](t, p.c.call("workspace/symbol", `{"query": "pkgsFor"}`))
	if len(workspace) != 1 || workspace[0].Name != "pkgsFor" || workspace[0].Location != p.at(name, "pkgsFor", 0) {
		t.Errorf("workspace symbols = %+v", workspace)
	}

	folds := decodeResult[[]lsp.FoldingRange](t, p.c.call("textDocument/foldingRange", `{"textDocument": {"uri": "`+p.uri(name)+`"}}`))
	lastLine := uint32(strings.Count(strings.TrimRight(text, "\n"), "\n"))
	outputsLine := uint32(strings.Count(text[:strings.Index(text, "outputs =\n")], "\n")) + 1
	if !slices.ContainsFunc(folds, func(f lsp.FoldingRange) bool { return f.StartLine == 0 && f.EndLine == lastLine }) {
		t.Errorf("folds = %+v, want the file's attribute set", folds)
	}
	if !slices.ContainsFunc(folds, func(f lsp.FoldingRange) bool { return f.StartLine == outputsLine && f.EndLine > outputsLine+1 }) {
		t.Errorf("folds = %+v, want the outputs function on line %d", folds, outputsLine)
	}

	edit := decodeResult[lsp.WorkspaceEdit](t, p.c.call("textDocument/rename", p.params(name, "revision", 0, `, "newName": "rev"`)))
	if got := editTexts(edit); !slices.Equal(got, []string{"rev", "rev"}) {
		t.Errorf("renaming revision edits %q", got)
	}
	edit = decodeResult[lsp.WorkspaceEdit](t, p.c.call("textDocument/rename", p.params(name, "system:", 0, `, "newName": "platform"`)))
	if got := editTexts(edit); !slices.Equal(got, []string{"platform", "system = platform;"}) {
		t.Errorf("renaming the argument of pkgsFor edits %q", got)
	}
}

func TestNixFlakePartsAnswersWithinEachFile(t *testing.T) {
	t.Parallel()

	const name = "default.nix"
	text := corpusFile(t, "flake-parts", "nix", "packages", name)
	p := openNativeProject(t, nativeCapabilities, map[string]string{name: text}, name)

	if got, want := decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params(name, "config.project.version", 0, ""))), []lsp.Location{p.at(name, "config", 0)}; !slices.Equal(got, want) {
		t.Errorf("definition of the outer config = %+v, want %+v", got, want)
	}
	if got, want := decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params(name, "config.packages.mirrorctl", 0, ""))), []lsp.Location{p.at(name, "config", 2)}; !slices.Equal(got, want) {
		t.Errorf("definition of the shadowing config = %+v, want %+v", got, want)
	}
	refs := decodeResult[[]lsp.Location](t, p.c.call("textDocument/references", p.params(name, "projectVersion", 0, `, "context": {"includeDeclaration": false}`)))
	if want := []lsp.Location{p.at(name, "projectVersion", 1), p.at(name, "projectVersion", 2)}; !slices.Equal(refs, want) {
		t.Errorf("references of projectVersion = %+v, want %+v", refs, want)
	}
	symbols := decodeResult[[]lsp.DocumentSymbol](t, p.c.call("textDocument/documentSymbol", `{"textDocument": {"uri": "`+p.uri(name)+`"}}`))
	perSystem := childSymbol(t, symbols, "perSystem")
	if perSystem.Kind != lsp.SymbolFunction {
		t.Errorf("perSystem has kind %v, want a function", perSystem.Kind)
	}
	childSymbol(t, childSymbol(t, perSystem.Children, "packages").Children, "platform-note")
	refused := p.c.call("textDocument/rename", p.params(name, "platform-note", 0, `, "newName": "note"`))
	if refused.Error == nil || !strings.Contains(refused.Error.Message, "may read it by its name") {
		t.Errorf("renaming an attribute of the module's result = %+v", refused)
	}
}

func TestNixFlakePartsShellReachesItsAttribute(t *testing.T) {
	t.Parallel()

	const name = "shell.nix"
	text := corpusFile(t, "flake-parts", "nix", name)
	p := openNativeProject(t, nativeCapabilities, map[string]string{name: text}, name)
	if got, want := decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params(name, "MIRROR_ROOT", 1, ""))), []lsp.Location{p.at(name, "MIRROR_ROOT", 0)}; !slices.Equal(got, want) {
		t.Errorf("definition of $MIRROR_ROOT = %+v, want %+v", got, want)
	}
	if got, want := decodeResult[[]lsp.Location](t, p.c.call("textDocument/definition", p.params(name, "system}", 0, ""))), []lsp.Location{p.at(name, "system", 0)}; !slices.Equal(got, want) {
		t.Errorf("definition of ${system} = %+v, want %+v", got, want)
	}
}

func TestNixRenameThroughTheServer(t *testing.T) {
	t.Parallel()

	const name = "default.nix"
	text := "let\n  value = 1;\n  other = 2;\nin {\n  inherit value;\n  sum = value + other;\n}\n"
	p := openNativeProject(t, nativeCapabilities, map[string]string{name: text}, name)

	prepared := decodeResult[lsp.PrepareRenameResult](t, p.c.call("textDocument/prepareRename", p.params(name, "value", 2, "")))
	if prepared.Placeholder != "value" || prepared.Range != p.at(name, "value", 2).Range {
		t.Errorf("prepareRename = %+v", prepared)
	}
	edit := decodeResult[lsp.WorkspaceEdit](t, p.c.call("textDocument/rename", p.params(name, "value", 0, `, "newName": "amount"`)))
	if got := editTexts(edit); !slices.Equal(got, []string{"amount", "value = amount;", "amount"}) {
		t.Errorf("rename edits %q", got)
	}
	for _, tt := range []struct{ newName, message string }{
		{"let", "not a valid"},
		{"other", "already"},
	} {
		response := p.c.call("textDocument/rename", p.params(name, "value", 0, `, "newName": "`+tt.newName+`"`))
		if response.Error == nil || !strings.Contains(response.Error.Message, tt.message) {
			t.Errorf("renaming to %s = %+v", tt.newName, response)
		}
	}
	refused := p.c.call("textDocument/prepareRename", p.params(name, "sum", 0, ""))
	if refused.Error == nil || !strings.Contains(refused.Error.Message, "may read it by its name") {
		t.Errorf("preparing to rename an attribute of the result = %+v", refused)
	}
}

func TestNixSelectionRangesComposeWithTheShell(t *testing.T) {
	t.Parallel()

	const name = "default.nix"
	text := "{\n  a = [ one ];\n  buildPhase = ''\n    echo $out\n  '';\n}\n"
	p := openNativeProject(t, nativeCapabilities, map[string]string{name: text}, name)
	oneLine, oneAt := p.pos(name, "one", 0)
	outLine, outAt := p.pos(name, "$out", 0)
	ranges := decodeResult[[]lsp.SelectionRange](t, p.c.call("textDocument/selectionRange", fmt.Sprintf(`{"textDocument": {"uri": %q}, "positions": [{"line": %d, "character": %d}, {"line": %d, "character": %d}]}`, p.uri(name), oneLine, oneAt+1, outLine, outAt+1)))
	chain := func(r *lsp.SelectionRange) []string {
		var out []string
		for ; r != nil; r = r.Parent {
			out = append(out, text[offsetOf(text, r.Range.Start):offsetOf(text, r.Range.End)])
		}
		return out
	}
	binding := "buildPhase = ''\n    echo $out\n  '';"
	whole := "{\n  a = [ one ];\n  " + binding + "\n}"
	if got, want := chain(&ranges[0]), []string{"one", "[ one ]", "a = [ one ];", whole, whole + "\n"}; !slices.Equal(got, want) {
		t.Errorf("selection in Nix = %q, want %q", got, want)
	}
	got := chain(&ranges[1])
	for _, want := range []string{"$out", "''\n    echo $out\n  ''", binding, whole, whole + "\n"} {
		if !slices.Contains(got, want) {
			t.Errorf("selection in the shell = %q, want it to hold %q", got, want)
		}
	}
	for i := 1; i < len(got); i++ {
		if !strings.Contains(got[i], got[i-1]) {
			t.Errorf("selection in the shell does not nest at %d: %q", i, got)
		}
	}
}

func TestNixShellSymbolsNestInTheBindingThatHoldsThem(t *testing.T) {
	t.Parallel()

	const name = "default.nix"
	text := "stdenv.mkDerivation {\n  buildPhase = ''\n    helper() { echo hi; }\n    helper\n  '';\n}\n"
	p := openNativeProject(t, nativeCapabilities, map[string]string{name: text}, name)
	symbols := decodeResult[[]lsp.DocumentSymbol](t, p.c.call("textDocument/documentSymbol", `{"textDocument": {"uri": "`+p.uri(name)+`"}}`))
	if got := symbolNames(childSymbol(t, symbols, "buildPhase").Children); !slices.Contains(got, "helper") {
		t.Errorf("buildPhase holds %q, want helper", got)
	}
}

func TestNixFoldsCoverLanguageAndComments(t *testing.T) {
	t.Parallel()

	const name = "default.nix"
	text := "# one\n# two\nlet\n  a = [\n    1\n  ];\nin {\n  b = ''\n    text\n  '';\n}\n"
	p := openNativeProject(t, nativeCapabilities, map[string]string{name: text}, name)
	got := decodeResult[[]lsp.FoldingRange](t, p.c.call("textDocument/foldingRange", `{"textDocument": {"uri": "`+p.uri(name)+`"}}`))
	want := []lsp.FoldingRange{
		{StartLine: 0, EndLine: 1, Kind: lsp.FoldingComment},
		{StartLine: 2, EndLine: 5},
		{StartLine: 3, EndLine: 5},
		{StartLine: 6, EndLine: 10},
		{StartLine: 7, EndLine: 9},
	}
	if !slices.Equal(got, want) {
		t.Errorf("folds = %+v, want %+v", got, want)
	}
}

func markedPosition(t *testing.T, src string) (string, int, int) {
	t.Helper()
	at := strings.Index(src, "|")
	if at < 0 {
		t.Fatalf("%q has no cursor", src)
	}
	before := src[:at]
	start := strings.LastIndexByte(before, '\n') + 1
	return before + src[at+1:], strings.Count(before, "\n"), len(utf16.Encode([]rune(before[start:])))
}

func openMarkedNix(t *testing.T, src string, extra map[string]string) (*nativeProject, string) {
	t.Helper()
	text, line, character := markedPosition(t, src)
	files := map[string]string{"default.nix": text}
	maps.Copy(files, extra)
	p := openNativeProject(t, nativeCapabilities, files, "default.nix")
	return p, positionParams(p.uri("default.nix"), line, character, "")
}

func nixCompletionLabels(t *testing.T, src string, extra map[string]string) []string {
	t.Helper()
	p, params := openMarkedNix(t, src, extra)
	list := decodeResult[lsp.CompletionList](t, p.c.call("textDocument/completion", params))
	labels := make([]string, len(list.Items))
	for i, it := range list.Items {
		labels[i] = it.Label
	}
	return labels
}

func TestNixCompletionThroughTheClient(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src string
		want, not []string
	}{
		{name: "after let", src: "let |", want: []string{"in", "inherit"}, not: []string{"map"}},
		{name: "in a set", src: "{ a = 1; | }", want: []string{"inherit"}, not: []string{"a"}},
		{name: "past a scope's end", src: "let a = 1; in |", want: []string{"a", "map", "let"}, not: []string{"__add"}},
		{name: "at x.", src: "let x = { one = 1; }; in x.|", want: []string{"one"}, not: []string{"map"}},
		{name: "at builtins.", src: "builtins.|", want: []string{"attrNames"}, not: []string{"__attrNames"}},
		{name: "an alias when typed", src: "__attr|", want: []string{"__attrNames"}},
		{name: "in a pattern", src: "{ a, | }: a", not: []string{"a", "map"}},
		{name: "keys of a call", src: "let f = { one }: one; in f { | }", want: []string{"one", "inherit"}},
		{name: "in ${...} in a shell string", src: "let nixName = 1; in { buildPhase = ''\n  echo ${|}\n''; }", want: []string{"nixName", "map"}, not: []string{"PATH"}},
		{name: "a broken file", src: "let a = 1; b = |\nin {", want: []string{"a", "b"}},
		{name: "after non-ASCII", src: "let a = \"é😀\"; b = \"é😀${|}\"; in a", want: []string{"a", "b"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := nixCompletionLabels(t, tt.src, nil)
			for _, want := range tt.want {
				if !slices.Contains(got, want) {
					t.Errorf("%q lacks %q in %q", tt.src, want, got)
				}
			}
			for _, not := range tt.not {
				if slices.Contains(got, not) {
					t.Errorf("%q offers %q", tt.src, not)
				}
			}
		})
	}
}

func TestNixCompletionReadsAnotherFile(t *testing.T) {
	t.Parallel()

	y := map[string]string{"y.nix": "{ stdenv, lib ? null }: 1\n", "dir/default.nix": "{ fromDir }: 1\n"}
	for _, tt := range []struct{ src, want string }{
		{"callPackage ./y.nix { | }", "stdenv"},
		{"import ./y.nix { | }", "lib"},
		{"callPackage ./dir { | }", "fromDir"},
	} {
		if got := nixCompletionLabels(t, tt.src, y); !slices.Contains(got, tt.want) {
			t.Errorf("%q lacks %q in %q", tt.src, tt.want, got)
		}
	}
	if got := nixCompletionLabels(t, "callPackage ./missing.nix { | }", y); !slices.Equal(got, []string{"inherit"}) {
		t.Errorf("a missing file offers %q", got)
	}
}

func TestNixCompletionEditsTheWordWithUTF16Positions(t *testing.T) {
	t.Parallel()

	p, params := openMarkedNix(t, "[ \"é😀\" ma|p ]", nil)
	list := decodeResult[lsp.CompletionList](t, p.c.call("textDocument/completion", params))
	i := slices.IndexFunc(list.Items, func(it lsp.CompletionItem) bool { return it.Label == "map" })
	if i < 0 {
		t.Fatalf("items = %+v", list.Items)
	}
	const start = 8
	want := lsp.Range{Start: lsp.Position{Line: 0, Character: start}, End: lsp.Position{Line: 0, Character: start + 3}}
	if got := list.Items[i].TextEdit; got == nil || got.Insert != want || got.NewText != "map" {
		t.Errorf("edit = %+v, want %+v", got, want)
	}
}

func TestNixCompletionResolvesDocsLazily(t *testing.T) {
	t.Parallel()

	p, params := openMarkedNix(t, "let\n  # Says one.\n  one = 1;\nin |", nil)
	list := decodeResult[lsp.CompletionList](t, p.c.call("textDocument/completion", params))
	for _, want := range []struct{ label, doc string }{{"one", "Says one."}, {"map", "`f`"}, {"let", "mutually recursive"}} {
		i := slices.IndexFunc(list.Items, func(it lsp.CompletionItem) bool { return it.Label == want.label })
		if i < 0 {
			t.Fatalf("%q is not in %+v", want.label, list.Items)
		}
		item := list.Items[i]
		if item.Documentation != nil {
			t.Errorf("%q arrives with its documentation", want.label)
		}
		raw, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		resolved := decodeResult[lsp.CompletionItem](t, p.c.call("completionItem/resolve", string(raw)))
		if resolved.Documentation == nil || !strings.Contains(resolved.Documentation.Value, want.doc) {
			t.Errorf("resolved %q = %+v", want.label, resolved)
		}
	}
}

func TestNixHoverThroughTheClient(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src string
		want      []string
	}{
		{name: "a builtin", src: "ma|p", want: []string{"map f list", "Takes 2 arguments."}},
		{name: "a member of builtins", src: "builtins.ad|d", want: []string{"add e1 e2", "Adds two numbers."}},
		{name: "a keyword", src: "l|et a = 1; in a", want: []string{"mutually recursive"}},
		{name: "a binding with its comment", src: "let\n  # Says one.\n  one = 1;\nin on|e", want: []string{"```nix\none = 1;\n```", "Says one."}},
		{name: "a binding read through a set", src: "let s = {\n  # Says one.\n  one = 1;\n}; in s.on|e", want: []string{"one = 1;", "Says one."}},
		{name: "a formal with its comment", src: "{\n  # Says one.\n  one,\n}: on|e", want: []string{"one", "Says one."}},
		{name: "a builtin inside ${...}", src: "{ buildPhase = ''\n  echo ${ma|p}\n''; }", want: []string{"map f list"}},
		{name: "after non-ASCII", src: "[ \"é😀\" ma|p ]", want: []string{"map f list"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, params := openMarkedNix(t, tt.src, nil)
			hover := decodeResult[*lsp.Hover](t, p.c.call("textDocument/hover", params))
			if hover == nil {
				t.Fatalf("%q has no hover", tt.src)
			}
			for _, want := range tt.want {
				if !strings.Contains(hover.Contents.Value, want) {
					t.Errorf("%q lacks %q:\n%s", tt.src, want, hover.Contents.Value)
				}
			}
		})
	}
}

func TestNixSignatureHelpThroughTheClient(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src, label string
		active           uint32
		param            string
	}{
		{name: "a builtin", src: "map a |", label: "map f list", active: 1, param: "list"},
		{name: "curried", src: "let f = a: b: a; in f 1 |", label: "f a b", active: 1, param: "b"},
		{name: "a set pattern", src: "let f = { one, two ? \"é😀\" }: one; in f { one = 1; | }", label: "f { one, two ? \"é😀\" }", active: 1, param: "two ? \"é😀\""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, params := openMarkedNix(t, tt.src, nil)
			help := decodeResult[*lsp.SignatureHelp](t, p.c.call("textDocument/signatureHelp", params))
			if help == nil || len(help.Signatures) != 1 || help.Signatures[0].Label != tt.label || help.ActiveParameter == nil || *help.ActiveParameter != tt.active {
				t.Fatalf("%q = %+v", tt.src, help)
			}
			units := utf16.Encode([]rune(tt.label))
			bounds := help.Signatures[0].Parameters[tt.active].Label
			if got := string(utf16.Decode(units[bounds[0]:bounds[1]])); got != tt.param {
				t.Errorf("%q highlights %q, want %q", tt.src, got, tt.param)
			}
		})
	}
}

func TestNixIgnoreQuickFixes(t *testing.T) {
	t.Parallel()

	const name = "default.nix"
	files := map[string]string{name: "let\n  unused = 1;\nin 2\n"}
	caps := `"capabilities": {"textDocument": {"diagnostic": {}, "codeAction": {"codeActionLiteralSupport": {"codeActionKind": {"valueSet": ["quickfix"]}}}}, "workspace": {"workspaceEdit": {"documentChanges": true}}}`
	p := openNativeProject(t, caps, files, name)
	diags := pullDiagnostics(t, p.c, p.uri(name))
	raw, err := json.Marshal(diags)
	if err != nil {
		t.Fatal(err)
	}
	r := p.at(name, "unused", 0).Range
	params := fmt.Sprintf(`{"textDocument": {"uri": %q}, "range": {"start": {"line": %d, "character": %d}, "end": {"line": %d, "character": %d}}, "context": {"diagnostics": %s}}`, p.uri(name), r.Start.Line, r.Start.Character, r.End.Line, r.End.Character, raw)
	actions := decodeResult[[]lsp.CodeAction](t, p.c.call("textDocument/codeAction", params))
	got := map[string]lsp.CodeAction{}
	for _, a := range actions {
		got[a.Title] = a
	}
	for title, newText := range map[string]string{
		"Ignore SL8005 here":         "  # shellcheck disable=SL8005\n",
		"Ignore SL8005 in this file": "# shellcheck disable=SL8005\n",
	} {
		action, ok := got[title]
		if !ok {
			t.Fatalf("%q is not in %v", title, slices.Collect(maps.Keys(got)))
		}
		if len(action.Diagnostics) != 1 || action.Diagnostics[0].Code != "SL8005" || action.Edit == nil || len(action.Edit.DocumentChanges) != 1 || action.Edit.DocumentChanges[0].Edits[0].NewText != newText {
			t.Errorf("%q = %+v", title, action)
		}
	}
	if _, ok := got["Remove `unused`"]; !ok {
		t.Errorf("the fix of the finding is gone: %v", slices.Collect(maps.Keys(got)))
	}
}

func TestNixDirectivesReachDiagnostics(t *testing.T) {
	t.Parallel()

	const name = "default.nix"
	files := map[string]string{name: "# shellcheck disable=nonsense\n# shellcheck disable=SL8006\n1\n"}
	codes := func(capabilities string) []string {
		p := openNativeProject(t, capabilities, files, name)
		var out []string
		for _, d := range pullDiagnostics(t, p.c, p.uri(name)) {
			out = append(out, d.Code)
		}
		slices.Sort(out)
		return out
	}
	if got := codes(nativeCapabilities); !slices.Equal(got, []string{"SC1125"}) {
		t.Errorf("by default = %q", got)
	}
	strict := nativeCapabilities + `, "initializationOptions": {"lint": {"profile": "strict"}}`
	if got := codes(strict); !slices.Equal(got, []string{"SC1125", "SL2009"}) {
		t.Errorf("under the strict profile = %q", got)
	}
}

func TestNixFindingOffersIgnoringItInTheWorkspace(t *testing.T) {
	t.Parallel()

	const name = "default.nix"
	files := map[string]string{name: "let unused = 1; in 2\n"}
	caps := `"capabilities": {"textDocument": {"diagnostic": {}, "codeAction": {"codeActionLiteralSupport": {"codeActionKind": {"valueSet": ["quickfix"]}}}}, "workspace": {"workspaceEdit": {"documentChanges": true, "resourceOperations": ["create"]}}}`
	p := openNativeProject(t, caps, files, name)
	r := p.at(name, "unused", 0).Range
	params := fmt.Sprintf(`{"textDocument": {"uri": %q}, "range": {"start": {"line": %d, "character": %d}, "end": {"line": %d, "character": %d}}, "context": {"diagnostics": []}}`, p.uri(name), r.Start.Line, r.Start.Character, r.End.Line, r.End.Character)
	for _, action := range decodeResult[[]lsp.CodeAction](t, p.c.call("textDocument/codeAction", params)) {
		if action.Title == "Ignore SL8005 in this workspace" {
			if action.Edit == nil {
				t.Errorf("the action has no edit: %+v", action)
			}
			return
		}
	}
	t.Error("no action ignores the finding in the workspace")
}

func TestMemoryIsReleasedAfterABurstOrARest(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var allocated uint64
	var rests, frees int
	counts := func() (int, int) {
		mu.Lock()
		defer mu.Unlock()
		return rests, frees
	}
	counted := func(n *int) func() {
		return func() {
			mu.Lock()
			defer mu.Unlock()
			*n++
		}
	}
	m := &memoryRelease{quiet: time.Millisecond, rest: 300 * time.Millisecond, burst: 100, rested: counted(&rests), free: counted(&frees), allocated: func() uint64 {
		mu.Lock()
		defer mu.Unlock()
		return allocated
	}}
	defer m.stop()
	waitFor := func(what string, wantRests, wantFrees int) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			rest, free := counts()
			if rest == wantRests && free == wantFrees {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: %d rests and %d frees, want %d and %d", what, rest, free, wantRests, wantFrees)
			}
			time.Sleep(time.Millisecond)
		}
	}
	mu.Lock()
	allocated = 100
	mu.Unlock()
	m.schedule()
	waitFor("a burst returns memory once quiet and keeps the caches", 0, 1)
	m.schedule()
	time.Sleep(5 * time.Millisecond)
	if rest, free := counts(); rest != 0 || free != 1 {
		t.Errorf("a pause after little work released memory: %d rests, %d frees", rest, free)
	}
	waitFor("a rest releases the caches and the memory", 1, 2)
}
