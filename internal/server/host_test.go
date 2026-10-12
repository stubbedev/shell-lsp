package server

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/lsp"
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

func TestMakefileRequestsOutsideRecipesFindNothing(t *testing.T) {
	t.Parallel()

	c := openMakefile(t, `"capabilities": {}`)
	at := func(line, character int) string {
		return positionParams(makefileURI, line, character, "")
	}
	for _, method := range []string{"textDocument/hover", "textDocument/definition", "textDocument/references", "textDocument/signatureHelp", "textDocument/documentHighlight"} {
		params := at(1, 2)
		if method == "textDocument/references" {
			params = positionParams(makefileURI, 1, 2, `, "context": {"includeDeclaration": true}`)
		}
		if msg := c.call(method, params); msg.Error != nil || string(msg.Result) != "null" {
			t.Errorf("%s on the FILES assignment = %s, %v, want null", method, msg.Result, msg.Error)
		}
	}
	if items := decodeResult[lsp.CompletionList](t, c.call("textDocument/completion", at(1, 2))).Items; len(items) != 0 {
		t.Errorf("completion on the FILES assignment = %v", items)
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
	got := decodeResult[documentDialect](t, c.call(documentDialectMethod, `{"textDocument": {"uri": "`+makefileURI+`"}}`))
	want := documentDialect{Dialect: "bash", Source: "hostSetting", Evidence: "SHELL := /bin/bash", Description: "from SHELL := /bin/bash", Dialects: []string{}}
	if !equalDialect(got, want) {
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

func TestMakefileSemanticTokensStayInRecipes(t *testing.T) {
	t.Parallel()

	c := openMakefile(t, `"capabilities": {}`)
	data := decodeResult[lsp.SemanticTokens](t, c.call("textDocument/semanticTokens/full", `{"textDocument": {"uri": "`+makefileURI+`"}}`)).Data
	if len(data) == 0 {
		t.Fatal("no semantic tokens for the recipes")
	}
	var line, start uint32
	for i := 0; i < len(data); i += 5 {
		if data[i] > 0 {
			line += data[i]
			start = data[i+1]
		} else {
			start += data[i+1]
		}
		if line < 4 || start == 0 || start+data[i+2] > uint32(len(makefileLines[line])) {
			t.Errorf("token %d at %d:%d+%d is outside the recipes", i/5, line, start, data[i+2])
		}
	}
}
