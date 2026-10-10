package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

func completionLabels(list lsp.CompletionList) []string {
	out := make([]string, 0, len(list.Items))
	for _, it := range list.Items {
		out = append(out, it.Label)
	}
	return out
}

func TestCompletionRequest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lib.sh"), []byte("x=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	uri := fileuri.FromPath(filepath.Join(dir, "main.sh"))
	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}, "completion": {"completionItem": {"snippetSupport": true}}}}}`)
	c.notify("textDocument/didOpen", openParams(uri, "greeting=hi\necho \"$gr\"\nsource ./li\nwhi\n"))

	at := func(line, character int) lsp.CompletionList {
		t.Helper()
		params, err := json.Marshal(lsp.CompletionParams{
			TextDocument: lsp.TextDocumentIdentifier{URI: uri},
			Position:     lsp.Position{Line: uint32(line), Character: uint32(character)},
		})
		if err != nil {
			t.Fatal(err)
		}
		return decodeResult[lsp.CompletionList](t, c.call("textDocument/completion", string(params)))
	}

	vars := at(1, 9)
	if !slices.Contains(completionLabels(vars), "greeting") {
		t.Fatalf("variables = %v", completionLabels(vars))
	}
	greeting := vars.Items[slices.IndexFunc(vars.Items, func(it lsp.CompletionItem) bool { return it.Label == "greeting" })]
	want := lsp.Range{Start: lsp.Position{Line: 1, Character: 7}, End: lsp.Position{Line: 1, Character: 9}}
	if greeting.TextEdit == nil || greeting.TextEdit.Range != want || greeting.Kind != lsp.CompletionVariable {
		t.Errorf("greeting item = %+v", greeting)
	}

	paths := at(2, 11)
	if len(paths.Items) == 0 || paths.Items[0].Label != "lib.sh" || paths.Items[0].TextEdit.NewText != "./lib.sh" {
		t.Errorf("paths = %+v", paths.Items)
	}

	keywords := at(3, 3)
	i := slices.IndexFunc(keywords.Items, func(it lsp.CompletionItem) bool { return it.Label == "while" })
	if i < 0 || keywords.Items[i].InsertTextFormat != lsp.InsertSnippet {
		t.Errorf("while completion = %v", completionLabels(keywords))
	}
}

func TestCompletionResolveDescribesCommands(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	got := decodeResult[lsp.CompletionItem](t, c.call("completionItem/resolve",
		`{"label": "definitely-not-a-command", "kind": 9, "insertTextFormat": 1, "data": {"kind": 1, "name": "definitely-not-a-command"}}`))
	if got.Documentation == nil || !strings.Contains(got.Documentation.Value, "not found in `PATH`") {
		t.Errorf("resolved item = %+v", got)
	}
	plain := decodeResult[lsp.CompletionItem](t, c.call("completionItem/resolve", `{"label": "x", "kind": 6, "insertTextFormat": 1}`))
	if plain.Label != "x" || plain.Documentation != nil {
		t.Errorf("item without data = %+v", plain)
	}
	bad := c.call("completionItem/resolve", `{"label": "x", "kind": 6, "insertTextFormat": 1, "data": {"other": 1}}`)
	if bad.Error == nil {
		t.Error("foreign completion data accepted")
	}
}

func TestExecutableCache(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for name, mode := range map[string]os.FileMode{"tool": 0o755, "data.txt": 0o644} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "tool"), filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	cache := &executableCache{}
	path := dir + string(os.PathListSeparator) + filepath.Join(dir, "missing")
	start := time.Now()
	if got := cache.list(path, start); !slices.Equal(got, []string{"linked", "tool"}) {
		t.Errorf("executables = %v", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "newtool"), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	later := start.Add(time.Second)
	if err := os.Chtimes(dir, later, later); err != nil {
		t.Fatal(err)
	}
	if got := cache.list(path, start.Add(workspace.DiskRecheck/2)); !slices.Equal(got, []string{"linked", "tool"}) {
		t.Errorf("executables within the recheck interval = %v, want the cached list", got)
	}
	if got := cache.list(path, start.Add(workspace.DiskRecheck)); !slices.Equal(got, []string{"linked", "newtool", "tool"}) {
		t.Errorf("executables after installing a tool = %v", got)
	}
}

func TestCompletionResolveDocumentsFunctionsAndVariables(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.bash", "# Says hi.\nhi() { :; }\nname=x\nh\necho $na\n"))
	list := decodeResult[lsp.CompletionList](t, c.call("textDocument/completion", positionParams("file:///w/a.bash", 3, 1, "")))
	i := slices.IndexFunc(list.Items, func(it lsp.CompletionItem) bool { return it.Label == "hi" })
	if i < 0 || list.Items[i].Documentation != nil {
		t.Fatalf("hi item = %+v", list.Items)
	}
	raw, err := json.Marshal(list.Items[i])
	if err != nil {
		t.Fatal(err)
	}
	resolved := decodeResult[lsp.CompletionItem](t, c.call("completionItem/resolve", string(raw)))
	if resolved.Documentation == nil || !strings.Contains(resolved.Documentation.Value, "Says hi.") {
		t.Errorf("resolved function = %+v", resolved)
	}
	vars := decodeResult[lsp.CompletionList](t, c.call("textDocument/completion", positionParams("file:///w/a.bash", 4, 8, "")))
	j := slices.IndexFunc(vars.Items, func(it lsp.CompletionItem) bool { return it.Label == "name" })
	if j < 0 {
		t.Fatalf("variables = %+v", vars.Items)
	}
	raw, err = json.Marshal(vars.Items[j])
	if err != nil {
		t.Fatal(err)
	}
	resolvedVar := decodeResult[lsp.CompletionItem](t, c.call("completionItem/resolve", string(raw)))
	if resolvedVar.Documentation == nil || !strings.Contains(resolvedVar.Documentation.Value, "global variable") {
		t.Errorf("resolved variable = %+v", resolvedVar)
	}
}
