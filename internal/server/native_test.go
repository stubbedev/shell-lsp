package server

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/fileuri"
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
	c, _ := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", `+capabilities+`}`)
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
	if hover == nil || !strings.Contains(hover.Contents.Value, "exported to recipes") {
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
	if bad.Error == nil || !strings.Contains(bad.Error.Message, "runs without arguments") {
		t.Errorf("running a recipe that needs an argument = %+v", bad)
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
