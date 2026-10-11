package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/structure"
)

const structureScript = "#!/bin/bash\n# one\n# two\ndeploy() {\n\tlocal target=x\n\techo \"$target\"\n}\nCOUNT=1\n"

func TestDocumentSymbolsHierarchical(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}, "documentSymbol": {"hierarchicalDocumentSymbolSupport": true}}}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.sh", structureScript))
	got := decodeResult[[]lsp.DocumentSymbol](t, c.call("textDocument/documentSymbol", `{"textDocument": {"uri": "file:///w/a.sh"}}`))
	if len(got) != 2 || got[0].Name != "deploy" || got[0].Kind != lsp.SymbolFunction || got[1].Name != "COUNT" {
		t.Fatalf("symbols = %+v", got)
	}
	if len(got[0].Children) != 1 || got[0].Children[0].Name != "target" || got[0].Children[0].Detail != "local variable" {
		t.Errorf("deploy children = %+v", got[0].Children)
	}
	want := lsp.Range{Start: lsp.Position{Line: 3}, End: lsp.Position{Line: 3, Character: 6}}
	if got[0].SelectionRange != want {
		t.Errorf("selection range = %+v, want %+v", got[0].SelectionRange, want)
	}
}

func TestDocumentSymbolsFlat(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.sh", structureScript))
	got := decodeResult[[]lsp.SymbolInformation](t, c.call("textDocument/documentSymbol", `{"textDocument": {"uri": "file:///w/a.sh"}}`))
	if len(got) != 3 || got[1].Name != "target" || got[1].ContainerName != "deploy" || got[1].Location.URI != "file:///w/a.sh" {
		t.Errorf("flat symbols = %+v", got)
	}
}

func TestFoldingRanges(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.sh", structureScript))
	got := decodeResult[[]lsp.FoldingRange](t, c.call("textDocument/foldingRange", `{"textDocument": {"uri": "file:///w/a.sh"}}`))
	want := []lsp.FoldingRange{{StartLine: 1, EndLine: 2, Kind: lsp.FoldingComment}, {StartLine: 3, EndLine: 5}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("folds = %+v, want %+v", got, want)
	}

	limited, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}, "foldingRange": {"rangeLimit": 1}}}}`)
	limited.notify("textDocument/didOpen", openParams("file:///w/a.sh", structureScript))
	if got := decodeResult[[]lsp.FoldingRange](t, limited.call("textDocument/foldingRange", `{"textDocument": {"uri": "file:///w/a.sh"}}`)); len(got) != 1 {
		t.Errorf("rangeLimit ignored: %+v", got)
	}
}

func TestWorkspaceSymbols(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "lib.sh"), []byte("deploy_all() { :; }\nLIB_DEPLOY=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _ := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", "capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openParams(fileuri.FromPath(filepath.Join(root, "main.sh")), structureScript))
	got := decodeResult[[]lsp.SymbolInformation](t, c.call("workspace/symbol", `{"query": "dep"}`))
	var names []string
	for _, s := range got {
		names = append(names, s.Name)
	}
	if len(got) != 3 || names[0] != "deploy" || names[1] != "deploy_all" || names[2] != "LIB_DEPLOY" {
		t.Errorf("workspace symbols = %v", names)
	}
}

const batsSuite = "#!/usr/bin/env bats\nsetup() {\n\tload helper\n}\n@test \"deploys the app\" {\n\trun deploy\n}\n"

func TestBatsTestSymbols(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "other.bats"), []byte("#!/usr/bin/env bats\n@test \"deploys twice\" { :; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	uri := fileuri.FromPath(filepath.Join(root, "suite.bats"))
	c, _ := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", "capabilities": {"textDocument": {"diagnostic": {}, "documentSymbol": {"hierarchicalDocumentSymbolSupport": true}}}}`)
	c.notify("textDocument/didOpen", openParams(uri, batsSuite))

	symbols := decodeResult[[]lsp.DocumentSymbol](t, c.call("textDocument/documentSymbol", `{"textDocument": {"uri": "`+uri+`"}}`))
	if len(symbols) != 2 || symbols[0].Name != "setup" || symbols[0].Detail != "bats hook, runs before each test" {
		t.Fatalf("symbols = %+v", symbols)
	}
	test := symbols[1]
	want := lsp.Range{Start: lsp.Position{Line: 4, Character: 6}, End: lsp.Position{Line: 4, Character: 23}}
	if test.Name != "deploys the app" || test.Kind != lsp.SymbolMethod || test.Detail != "test" || test.SelectionRange != want || test.Range.End.Line != 6 {
		t.Errorf("test symbol = %+v", test)
	}

	found := decodeResult[[]lsp.SymbolInformation](t, c.call("workspace/symbol", `{"query": "deploys"}`))
	var names []string
	for _, s := range found {
		if s.Kind == lsp.SymbolMethod {
			names = append(names, s.Name)
		}
	}
	if strings.Join(names, ",") != "deploys the app,deploys twice" {
		t.Errorf("workspace test symbols = %v", names)
	}
}

func TestMatches(t *testing.T) {
	t.Parallel()

	for query, want := range map[string]bool{"": true, "dpl": true, "DEP": true, "xyz": false, "deployx": false} {
		if got := structure.Matches(query, "deploy"); got != want {
			t.Errorf("Matches(%q, deploy) = %t", query, got)
		}
	}
}
