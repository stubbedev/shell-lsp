package server

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/actions"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/semantic"
)

const makefileURI = "file:///w/Makefile"

var makefileLines = []string{
	"SHELL := /bin/bash",
	"FILES = $(wildcard *.c)",
	"",
	"all: build",
	"\tfor f in $(FILES); do echo $$f; done",
	"\techo `date`",
	"\t@greeting=hi; echo \"$$greet\"",
}

func makefileRange(t *testing.T, line int, sub string) lsp.Range {
	t.Helper()

	i := strings.Index(makefileLines[line], sub)
	if i < 0 {
		t.Fatalf("%q is not on line %d of the Makefile", sub, line)
	}
	return lsp.Range{
		Start: lsp.Position{Line: uint32(line), Character: uint32(i)},
		End:   lsp.Position{Line: uint32(line), Character: uint32(i + len(sub))},
	}
}

func openMakefile(t *testing.T, capabilities string) *testClient {
	t.Helper()

	c, _ := startClient(t, `{`+capabilities+`}`)
	c.notify("textDocument/didOpen", openLanguageParams(makefileURI, "makefile", strings.Join(makefileLines, "\n")+"\n"))
	return c
}

func TestMakefileDiagnosticsPointIntoRecipes(t *testing.T) {
	t.Parallel()

	c := openMakefile(t, pullCapabilities)
	type located struct {
		code string
		at   lsp.Range
	}
	var got []located
	for _, d := range pullDiagnostics(t, c, makefileURI) {
		got = append(got, located{d.Code, d.Range})
	}
	backticks := makefileRange(t, 5, "`date`")
	want := []located{
		{"SC2086", makefileRange(t, 4, "$$f")},
		{"SC2005", backticks},
		{"SC2006", backticks},
		{"SC2046", backticks},
		{"SC2034", makefileRange(t, 6, "greeting=hi")},
		{"SC2154", makefileRange(t, 6, "$$greet")},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Makefile diagnostics\n got %+v\nwant %+v", got, want)
	}
}

func TestMakefileQuickFixIsWrittenInMakeSyntax(t *testing.T) {
	t.Parallel()

	c := openMakefile(t, actionCapabilities)
	diags := pullDiagnostics(t, c, makefileURI)
	i := slices.IndexFunc(diags, func(d lsp.Diagnostic) bool { return d.Code == "SC2006" })
	if i < 0 {
		t.Fatalf("no SC2006 among %+v", diags)
	}
	encoded, err := json.Marshal(lsp.CodeActionParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: makefileURI},
		Range:        diags[i].Range,
		Context:      lsp.CodeActionContext{Diagnostics: diags[i : i+1]},
	})
	if err != nil {
		t.Fatal(err)
	}
	var edits []lsp.TextEdit
	for _, a := range codeActions(t, c, string(encoded)) {
		if a.Edit == nil {
			t.Errorf("%q has no edit, although resolving is off for a Makefile", a.Title)
			continue
		}
		for _, change := range a.Edit.DocumentChanges {
			edits = append(edits, change.Edits...)
		}
	}
	want := lsp.TextEdit{Range: makefileRange(t, 5, "`date`"), NewText: "$$(date)"}
	if !slices.Contains(edits, want) {
		t.Errorf("edits = %+v, want one that is %+v", edits, want)
	}
	for _, e := range edits {
		if e.Range.Start.Line != 5 || strings.Contains(e.NewText, "\n") {
			t.Errorf("edit %+v reaches outside the recipe line", e)
		}
	}
}

func TestMakefileOffersOnlyDocumentActionsOutsideItsRecipes(t *testing.T) {
	t.Parallel()

	c := openMakefile(t, actionCapabilities)
	for _, at := range []lsp.Range{makefileRange(t, 1, "wildcard"), makefileRange(t, 3, "all")} {
		encoded, err := json.Marshal(lsp.CodeActionParams{TextDocument: lsp.TextDocumentIdentifier{URI: makefileURI}, Range: at})
		if err != nil {
			t.Fatal(err)
		}
		got := codeActions(t, c, string(encoded))
		if len(got) != 1 || got[0].Kind != lsp.CodeActionKind(actions.SourceFixAll) {
			t.Errorf("actions at %+v, outside every recipe = %+v, want only the document's fix-all", at, got)
		}
	}
}

func TestMakefileFixAllIsWrittenInMakeSyntax(t *testing.T) {
	t.Parallel()

	c := openMakefile(t, actionCapabilities)
	id := c.start("workspace/executeCommand", `{"command": "`+commandFixAll+`", "arguments": ["`+makefileURI+`"]}`)
	applied := c.await("workspace/applyEdit", func(m *jsonrpc.Message) bool { return m.IsRequest() && m.Method == "workspace/applyEdit" })
	var params lsp.ApplyWorkspaceEditParams
	if err := json.Unmarshal(applied.Params, &params); err != nil {
		t.Fatal(err)
	}
	if len(params.Edit.DocumentChanges) != 1 || params.Edit.DocumentChanges[0].TextDocument.URI != makefileURI {
		t.Fatalf("applyEdit = %+v, want edits to the Makefile", params)
	}
	want := lsp.TextEdit{Range: makefileRange(t, 5, "`date`"), NewText: "$$(date)"}
	if edits := params.Edit.DocumentChanges[0].Edits; !slices.Contains(edits, want) {
		t.Errorf("fix all edits = %+v, want one that is %+v", edits, want)
	}
	if err := c.conn.Reply(applied.ID, lsp.ApplyWorkspaceEditResult{Applied: true}, nil); err != nil {
		t.Fatal(err)
	}
	if done := c.awaitResponse(id, "workspace/executeCommand"); done.Error != nil {
		t.Errorf("executeCommand error = %+v", done.Error)
	}
}

func TestMakefileRequestsOutsideRecipesAnswerInMake(t *testing.T) {
	t.Parallel()

	c := openMakefile(t, `"capabilities": {}`)
	at := func(line, character int) string {
		return positionParams(makefileURI, line, character, "")
	}
	declaration := makefileRange(t, 1, "FILES")
	use := makefileRange(t, 4, "FILES")
	hover := decodeResult[*lsp.Hover](t, c.call("textDocument/hover", at(1, 2)))
	if hover == nil || hover.Range == nil || *hover.Range != declaration || !strings.Contains(hover.Contents.Value, "FILES = $(wildcard *.c)") {
		t.Errorf("hover on the FILES assignment = %+v, want the make variable", hover)
	}
	if got := decodeResult[[]lsp.Location](t, c.call("textDocument/definition", at(4, 13))); len(got) != 1 || got[0].Range != declaration {
		t.Errorf("definition of $(FILES) = %+v, want the assignment at %+v", got, declaration)
	}
	refs := decodeResult[[]lsp.Location](t, c.call("textDocument/references", positionParams(makefileURI, 1, 2, `, "context": {"includeDeclaration": true}`)))
	if len(refs) != 2 || refs[0].Range != declaration || refs[1].Range != use {
		t.Errorf("references of FILES = %+v, want the assignment and $(FILES)", refs)
	}
	highlights := decodeResult[[]lsp.DocumentHighlight](t, c.call("textDocument/documentHighlight", at(4, 13)))
	if len(highlights) != 2 || highlights[0].Range != declaration || highlights[1].Range != use {
		t.Errorf("highlights of FILES = %+v", highlights)
	}
	if msg := c.call("textDocument/signatureHelp", at(1, 2)); msg.Error != nil || string(msg.Result) != "null" {
		t.Errorf("signatureHelp on the FILES assignment = %s, %v, want null", msg.Result, msg.Error)
	}
	if labels := completionLabels(decodeResult[lsp.CompletionList](t, c.call("textDocument/completion", at(1, 2)))); !slices.Contains(labels, "include") || !slices.Contains(labels, "ifeq") {
		t.Errorf("completion at the start of a make line = %v, want the directives", labels)
	}
	if hover := decodeResult[*lsp.Hover](t, c.call("textDocument/hover", at(4, 25))); hover == nil || hover.Range == nil || *hover.Range != makefileRange(t, 4, "echo") {
		t.Errorf("hover on echo in the recipe = %+v", hover)
	}
	if hover := c.call("textDocument/hover", at(6, 1)); string(hover.Result) != "null" {
		t.Errorf("hover on the @ recipe prefix = %s, want null", hover.Result)
	}
}

func TestMakefileCompletionReplacesHostText(t *testing.T) {
	t.Parallel()

	c := openMakefile(t, `"capabilities": {}`)
	name := makefileRange(t, 6, "greet\"")
	name.End.Character--
	list := decodeResult[lsp.CompletionList](t, c.call("textDocument/completion", positionParams(makefileURI, 6, int(name.End.Character), "")))
	i := slices.IndexFunc(list.Items, func(it lsp.CompletionItem) bool { return it.Label == "greeting" })
	if i < 0 {
		t.Fatalf("completion = %v", completionLabels(list))
	}
	if edit := list.Items[i].TextEdit; edit == nil || edit.Insert != name || edit.NewText != "greeting" {
		t.Errorf("greeting edit = %+v, want %q over %+v", edit, "greeting", name)
	}
	for _, it := range list.Items {
		if it.InsertTextFormat == lsp.InsertSnippet {
			t.Errorf("%q is a snippet, whose $ placeholders make would expand", it.Label)
		}
	}
}

func TestMakefileDialectComesFromShell(t *testing.T) {
	t.Parallel()

	c := openMakefile(t, `"capabilities": {}`)
	got := askDialect(t, c, makefileURI)
	want := dialectAnswer{Dialect: "bash", Source: "hostSetting", Evidence: "SHELL := /bin/bash", Description: "from SHELL := /bin/bash"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Makefile dialect = %+v, want %+v", got, want)
	}
	if msg := c.call(setDialectMethod, `{"textDocument": {"uri": "`+makefileURI+`"}, "dialect": "zsh"}`); msg.Error == nil {
		t.Errorf("setDialect wrote a directive into a Makefile: %s", msg.Result)
	}
}

func TestMakefileIsNotFormatted(t *testing.T) {
	t.Parallel()

	c := openMakefile(t, `"capabilities": {}`)
	edits := decodeResult[[]lsp.TextEdit](t, c.call("textDocument/formatting", `{"textDocument": {"uri": "`+makefileURI+`"}, "options": {"tabSize": 4, "insertSpaces": true}}`))
	if len(edits) != 0 {
		t.Errorf("formatting a Makefile = %+v", edits)
	}
}

type placed struct {
	line, start, length uint32
	kind                string
}

func decodeTokens(t *testing.T, data []uint32) []placed {
	t.Helper()
	var out []placed
	var line, start uint32
	for i := 0; i < len(data); i += 5 {
		if data[i] > 0 {
			line += data[i]
			start = data[i+1]
		} else {
			start += data[i+1]
		}
		out = append(out, placed{line: line, start: start, length: data[i+2], kind: semantic.TypeNames[data[i+3]]})
	}
	return out
}

func TestMakefileSemanticTokensCoverMakeAndShell(t *testing.T) {
	t.Parallel()

	c := openMakefile(t, `"capabilities": {}`)
	tokens := decodeTokens(t, decodeResult[lsp.SemanticTokens](t, c.call("textDocument/semanticTokens/full", `{"textDocument": {"uri": "`+makefileURI+`"}}`)).Data)
	token := func(line int, sub, kind string) placed {
		r := makefileRange(t, line, sub)
		return placed{line: r.Start.Line, start: r.Start.Character, length: r.End.Character - r.Start.Character, kind: kind}
	}
	for _, want := range []placed{
		token(0, "SHELL", "variable"),
		token(1, "FILES", "variable"),
		token(1, "wildcard", "function"),
		token(3, "all", "function"),
		token(3, "build", "function"),
		token(4, "for", "keyword"),
		token(4, "FILES", "variable"),
	} {
		if !slices.Contains(tokens, want) {
			t.Errorf("no %+v among %+v", want, tokens)
		}
	}
	for i := 1; i < len(tokens); i++ {
		prev, cur := tokens[i-1], tokens[i]
		if cur.line == prev.line && cur.start < prev.start+prev.length {
			t.Errorf("token %+v overlaps %+v", cur, prev)
		}
	}
}

func TestJustfileDiagnosticsAndFixes(t *testing.T) {
	t.Parallel()

	const uri = "file:///w/justfile"
	lines := []string{
		"set shell := [\"bash\", \"-uc\"]",
		"deploy env:",
		"    @echo `date` {{env}} $1",
		"    #!/not-a-shebang-after-the-first-line",
	}
	c, _ := startClient(t, `{`+actionCapabilities+`}`)
	c.notify("textDocument/didOpen", openLanguageParams(uri, "just", strings.Join(lines, "\n")+"\n"))
	at := func(line int, sub string) lsp.Range {
		i := strings.Index(lines[line], sub)
		return lsp.Range{Start: lsp.Position{Line: uint32(line), Character: uint32(i)}, End: lsp.Position{Line: uint32(line), Character: uint32(i + len(sub))}}
	}
	diags := pullDiagnostics(t, c, uri)
	var got []string
	for _, d := range diags {
		got = append(got, d.Code)
		if d.Range.Start.Line != 2 && d.Range.Start.Line != 3 {
			t.Errorf("%s at %+v is outside the recipe", d.Code, d.Range)
		}
	}
	if want := []string{"SC2006", "SC2046", "SC2086"}; !slices.Equal(got, want) {
		t.Errorf("justfile diagnostics = %v, want %v: %+v", got, want, diags)
	}
	i := slices.IndexFunc(diags, func(d lsp.Diagnostic) bool { return d.Code == "SC2006" })
	if i < 0 || diags[i].Range != at(2, "`date`") {
		t.Fatalf("SC2006 = %+v, want it on `date`", diags)
	}
	encoded, err := json.Marshal(lsp.CodeActionParams{TextDocument: lsp.TextDocumentIdentifier{URI: uri}, Range: diags[i].Range, Context: lsp.CodeActionContext{Diagnostics: diags[i : i+1]}})
	if err != nil {
		t.Fatal(err)
	}
	want := lsp.TextEdit{Range: at(2, "`date`"), NewText: "$(date)"}
	found := false
	for _, a := range codeActions(t, c, string(encoded)) {
		for _, change := range a.Edit.DocumentChanges {
			found = found || slices.Contains(change.Edits, want)
		}
	}
	if !found {
		t.Errorf("no action makes %+v", want)
	}
	dialect := askDialect(t, c, uri)
	if dialect.Selectable || dialect.Dialect != "bash" || dialect.Evidence != `set shell := ["bash", "-uc"]` {
		t.Errorf("justfile dialect = %+v", dialect)
	}
}

func TestNixShellStringsLintInPlace(t *testing.T) {
	t.Parallel()

	const uri = "file:///w/package.nix"
	lines := []string{
		"stdenv.mkDerivation {",
		"  pname = \"x\";",
		"  installPhase = ''",
		"    cp ${src}/bin/* $out/bin/`basename $1`",
		"  '';",
		"}",
	}
	c, _ := startClient(t, `{`+actionCapabilities+`}`)
	c.notify("textDocument/didOpen", openLanguageParams(uri, "nix", strings.Join(lines, "\n")+"\n"))
	at := func(sub string) lsp.Range {
		i := strings.Index(lines[3], sub)
		return lsp.Range{Start: lsp.Position{Line: 3, Character: uint32(i)}, End: lsp.Position{Line: 3, Character: uint32(i + len(sub))}}
	}
	diags := pullDiagnostics(t, c, uri)
	var got []string
	for _, d := range diags {
		got = append(got, d.Code)
	}
	if want := []string{"SC2086", "SC2006", "SC2046", "SC2086"}; !slices.Equal(got, want) {
		t.Fatalf("Nix diagnostics = %v, want %v: %+v", got, want, diags)
	}
	i := slices.IndexFunc(diags, func(d lsp.Diagnostic) bool { return d.Code == "SC2006" })
	if diags[0].Range != at("$out") || diags[i].Range != at("`basename $1`") {
		t.Errorf("diagnostics at %+v and %+v, want on $out and the backticks", diags[0].Range, diags[i].Range)
	}
	encoded, err := json.Marshal(lsp.CodeActionParams{TextDocument: lsp.TextDocumentIdentifier{URI: uri}, Range: diags[i].Range, Context: lsp.CodeActionContext{Diagnostics: diags[i : i+1]}})
	if err != nil {
		t.Fatal(err)
	}
	want := lsp.TextEdit{Range: at("`basename $1`"), NewText: "$(basename $1)"}
	found := false
	for _, a := range codeActions(t, c, string(encoded)) {
		for _, change := range a.Edit.DocumentChanges {
			found = found || slices.Contains(change.Edits, want)
		}
	}
	if !found {
		t.Errorf("no action makes %+v", want)
	}
	if hover := c.call("textDocument/hover", positionParams(uri, 3, strings.Index(lines[3], "src"), "")); string(hover.Result) != "null" {
		t.Errorf("hover on ${src} = %s, want null", hover.Result)
	}
}

func TestHostFilesOpenedAsShellscriptAreStillProjected(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		uri, text string
		want      []string
	}{
		{"file:///w/justfile", "db := env('DB', 'x')\n\n[private]\nx:\n    echo $1 {{db}}\n", []string{"SC2086"}},
		{makefileURI, strings.Join(makefileLines, "\n") + "\n", []string{"SC2005", "SC2006", "SC2034", "SC2046", "SC2086", "SC2154"}},
	} {
		c, _ := startClient(t, `{`+pullCapabilities+`}`)
		c.notify("textDocument/didOpen", openLanguageParams(tt.uri, "shellscript", tt.text))
		if got := codes(pullDiagnostics(t, c, tt.uri)); !slices.Equal(got, tt.want) {
			t.Errorf("%s opened as shellscript: diagnostics = %v, want %v", tt.uri, got, tt.want)
		}
	}
}
