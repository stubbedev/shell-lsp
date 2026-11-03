package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

func TestCodeLensAndCallHierarchy(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "lib.sh"), []byte("log() { echo \"$*\"; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	uri := fileuri.FromPath(filepath.Join(root, "main.sh"))
	c, _ := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", "capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openParams(uri, "source ./lib.sh\nNAME=x\nbuild() {\n\tlog \"$NAME\"\n}\nbuild\nbuild\n"))

	lenses := decodeResult[[]lsp.CodeLens](t, c.call("textDocument/codeLens", `{"textDocument": {"uri": "`+uri+`"}}`))
	if len(lenses) != 2 || lenses[0].Command != nil {
		t.Fatalf("lenses = %+v", lenses)
	}
	raw, err := json.Marshal(lenses[0])
	if err != nil {
		t.Fatal(err)
	}
	resolved := decodeResult[lsp.CodeLens](t, c.call("codeLens/resolve", string(raw)))
	if resolved.Command == nil || resolved.Command.Title != "2 references" || resolved.Command.Command != "shellLsp.showReferences" {
		t.Errorf("resolved lens = %+v", resolved)
	}
	refs := decodeResult[[]lsp.Location](t, c.call("workspace/executeCommand", `{"command": "shellLsp.showReferences", "arguments": [`+string(resolved.Command.Arguments[0])+`]}`))
	if len(refs) != 2 {
		t.Errorf("showReferences = %+v", refs)
	}

	items := decodeResult[[]lsp.CallHierarchyItem](t, c.call("textDocument/prepareCallHierarchy", positionParams(uri, 3, 2, "")))
	if len(items) != 1 || items[0].Name != "log" || items[0].URI != fileuri.FromPath(filepath.Join(root, "lib.sh")) {
		t.Fatalf("prepare = %+v", items)
	}
	rawItem, err := json.Marshal(items[0])
	if err != nil {
		t.Fatal(err)
	}
	incoming := decodeResult[[]lsp.CallHierarchyIncomingCall](t, c.call("callHierarchy/incomingCalls", `{"item": `+string(rawItem)+`}`))
	if len(incoming) != 1 || incoming[0].From.Name != "build" || len(incoming[0].FromRanges) != 1 || incoming[0].FromRanges[0].Start.Line != 3 {
		t.Errorf("incoming = %+v", incoming)
	}
	buildItem, err := json.Marshal(incoming[0].From)
	if err != nil {
		t.Fatal(err)
	}
	outgoing := decodeResult[[]lsp.CallHierarchyOutgoingCall](t, c.call("callHierarchy/outgoingCalls", `{"item": `+string(buildItem)+`}`))
	if len(outgoing) != 1 || outgoing[0].To.Name != "log" {
		t.Errorf("outgoing = %+v", outgoing)
	}
}

func TestInlineValueMonikerAndLinkedEditing(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.bash", "f() {\n\tlocal n=1\n\techo \"$n\"\n}\ncat <<EOF\nx\nEOF\n"))

	values := decodeResult[[]lsp.InlineValueVariableLookup](t, c.call("textDocument/inlineValue", `{"textDocument": {"uri": "file:///w/a.bash"}, "range": {"start": {"line": 0, "character": 0}, "end": {"line": 4, "character": 0}}, "context": {"frameId": 1, "stoppedLocation": {"start": {"line": 2, "character": 1}, "end": {"line": 2, "character": 5}}}}`))
	if len(values) != 1 || values[0].VariableName != "n" || !values[0].CaseSensitiveLookup {
		t.Errorf("inline values = %+v", values)
	}

	monikers := decodeResult[[]lsp.Moniker](t, c.call("textDocument/moniker", positionParams("file:///w/a.bash", 0, 0, "")))
	if len(monikers) != 1 || monikers[0].Kind != "export" || monikers[0].Unique != "project" || monikers[0].Scheme != "shell-lsp" {
		t.Errorf("monikers = %+v", monikers)
	}

	linked := decodeResult[lsp.LinkedEditingRanges](t, c.call("textDocument/linkedEditingRange", positionParams("file:///w/a.bash", 4, 7, "")))
	if len(linked.Ranges) != 2 || linked.Ranges[1].Start.Line != 6 {
		t.Errorf("linked editing = %+v", linked)
	}
	if none := c.call("textDocument/linkedEditingRange", positionParams("file:///w/a.bash", 1, 2, "")); string(none.Result) != "null" {
		t.Errorf("linked editing outside heredoc = %s", none.Result)
	}
}

func TestCallHierarchyKeepsTheChosenDefinition(t *testing.T) {
	t.Parallel()

	uri := "file:///w/redefine.sh"
	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openParams(uri, "first() { :; }\nsecond() { :; }\ndeploy() { first; }\ndeploy() { second; }\ndeploy\n"))

	items := decodeResult[[]lsp.CallHierarchyItem](t, c.call("textDocument/prepareCallHierarchy", positionParams(uri, 3, 0, "")))
	if len(items) != 1 || items[0].Name != "deploy" || items[0].Range.Start.Line != 3 {
		t.Fatalf("prepare = %+v", items)
	}
	raw, err := json.Marshal(items[0])
	if err != nil {
		t.Fatal(err)
	}
	outgoing := decodeResult[[]lsp.CallHierarchyOutgoingCall](t, c.call("callHierarchy/outgoingCalls", `{"item": `+string(raw)+`}`))
	if len(outgoing) != 1 || outgoing[0].To.Name != "second" {
		t.Errorf("outgoing calls of the second deploy = %+v, want only second", outgoing)
	}
	atCall := decodeResult[[]lsp.CallHierarchyItem](t, c.call("textDocument/prepareCallHierarchy", positionParams(uri, 4, 0, "")))
	if len(atCall) != 2 || atCall[0].Range.Start.Line != 2 || atCall[1].Range.Start.Line != 3 {
		t.Errorf("prepare on a call of a redefined function = %+v, want both definitions", atCall)
	}
}

func nixRunTitles(t *testing.T, p *nativeProject, name string) []string {
	t.Helper()
	var titles []string
	for _, l := range decodeResult[[]lsp.CodeLens](t, p.c.call("textDocument/codeLens", `{"textDocument": {"uri": "`+p.uri(name)+`"}}`)) {
		if l.Command != nil && l.Command.Command == commandRunRecipe {
			titles = append(titles, l.Command.Title)
		}
	}
	return titles
}

func TestNixFlakeRunLenses(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		corpus []string
		want   []string
	}{
		{[]string{"single-flake", "flake.nix"}, []string{"▶ nix build .#default", "▶ nix build .#logship", "▶ nix build .#logship-rotate", "▶ nix build .#manual", "▶ nix run .#default", "▶ nix flake check", "▶ Build dev shell .#default"}},
		{[]string{"flake-parts", "nix", "shell.nix"}, []string{"▶ Build dev shell .#default"}},
		{[]string{"flake-parts", "nix", "checks.nix"}, []string{"▶ nix flake check"}},
		{[]string{"flake-parts", "nix", "project.nix"}, nil},
	} {
		name := tt.corpus[len(tt.corpus)-1]
		p := openNativeProject(t, nativeCapabilities, map[string]string{name: corpusFile(t, tt.corpus...)}, name)
		if got := nixRunTitles(t, p, name); !slices.Equal(got, tt.want) {
			t.Errorf("run lenses of %v = %q, want %q", tt.corpus, got, tt.want)
		}
	}
}

func TestDevShellLensOpensATerminalWhenTheClientCan(t *testing.T) {
	t.Parallel()

	const name = "shell.nix"
	capabilities := `"capabilities": {"experimental": {"shellLsp": {"runsInTerminal": true}}}`
	p := openNativeProject(t, capabilities, map[string]string{name: corpusFile(t, "flake-parts", "nix", "shell.nix")}, name)
	lenses := decodeResult[[]lsp.CodeLens](t, p.c.call("textDocument/codeLens", `{"textDocument": {"uri": "`+p.uri(name)+`"}}`))
	i := slices.IndexFunc(lenses, func(l lsp.CodeLens) bool { return l.Command != nil && l.Command.Command == commandRunInTerminal })
	if i < 0 {
		t.Fatalf("no terminal lens among %+v", lenses)
	}
	var args terminalRunArgs
	if err := json.Unmarshal(lenses[i].Command.Arguments[0], &args); err != nil {
		t.Fatal(err)
	}
	if lenses[i].Command.Title != "▶ nix develop .#default" || !slices.Equal(args.Argv, []string{"nix", "develop", ".#default"}) || args.Cwd == "" {
		t.Errorf("terminal lens = %q %+v", lenses[i].Command.Title, args)
	}
}

func TestNixDocumentLinks(t *testing.T) {
	t.Parallel()

	const name = "default.nix"
	files := map[string]string{name: "# https://example.com/docs\n{ a = import ./b.nix; c = import ./sub; }\n", "b.nix": "1\n", "sub/default.nix": "2\n"}
	p := openNativeProject(t, nativeCapabilities, files, name)
	links := decodeResult[[]lsp.DocumentLink](t, p.c.call("textDocument/documentLink", `{"textDocument": {"uri": "`+p.uri(name)+`"}}`))
	var targets []string
	seen := map[lsp.Range]bool{}
	for _, l := range links {
		if seen[l.Range] {
			t.Errorf("two links over %+v: %+v", l.Range, links)
		}
		seen[l.Range] = true
		targets = append(targets, l.Target)
	}
	slices.Sort(targets)
	want := []string{p.uri("b.nix"), p.uri("sub/default.nix"), "https://example.com/docs"}
	slices.Sort(want)
	if !slices.Equal(targets, want) {
		t.Errorf("link targets = %q, want %q", targets, want)
	}
}

func TestNixCallHierarchyAcrossItsBindings(t *testing.T) {
	t.Parallel()

	const name = "default.nix"
	p := openNativeProject(t, nativeCapabilities, map[string]string{name: "let\n  g = y: y;\n  f = x: g x;\nin\n  f 1\n"}, name)
	items := decodeResult[[]lsp.CallHierarchyItem](t, p.c.call("textDocument/prepareCallHierarchy", p.params(name, "f", 0, "")))
	if len(items) != 1 || items[0].Name != "f" {
		t.Fatalf("prepared items = %+v", items)
	}
	item, err := json.Marshal(items[0])
	if err != nil {
		t.Fatal(err)
	}
	outgoing := decodeResult[[]lsp.CallHierarchyOutgoingCall](t, p.c.call("callHierarchy/outgoingCalls", `{"item": `+string(item)+`}`))
	if len(outgoing) != 1 || outgoing[0].To.Name != "g" {
		t.Fatalf("outgoing calls = %+v", outgoing)
	}
	callee, err := json.Marshal(outgoing[0].To)
	if err != nil {
		t.Fatal(err)
	}
	incoming := decodeResult[[]lsp.CallHierarchyIncomingCall](t, p.c.call("callHierarchy/incomingCalls", `{"item": `+string(callee)+`}`))
	if len(incoming) != 1 || incoming[0].From.Name != "f" {
		t.Errorf("incoming calls = %+v", incoming)
	}
}
