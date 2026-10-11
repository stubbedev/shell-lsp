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
	"github.com/stubbedev/shell-lsp/internal/manpage"
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

const (
	toolPage = `.TH TOOL 1
.SH NAME
tool \- manage widgets
.SH SYNOPSIS
.B tool
[\fB\-v\fR] \fICOMMAND\fR
.SH COMMANDS
.TP
\fBstart\fR \fIUNIT\fR...
Start units. They start in parallel.
.TP
\fBstop\fR \fIUNIT\fR...
Stop units.
.RS
.TP
.B \-\-force
kill them
.RE
.SH OPTIONS
.TP
.B \-v
be verbose
`
	toolExtraPage = `.TH TOOL-EXTRA 1
.SH NAME
tool-extra \- extra widget tricks
.SH SYNOPSIS
.B tool extra
[\fB\-x\fR]
.SH OPTIONS
.TP
.B \-x
do the extra trick
`
)

func withToolPages(t testing.TB) func(*Server) {
	t.Helper()
	dir := t.TempDir()
	for name, page := range map[string]string{"tool.1": toolPage, "tool-extra.1": toolExtraPage} {
		if err := os.MkdirAll(filepath.Join(dir, "man1"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "man1", name), []byte(page), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return func(s *Server) { s.usePages(manpage.NewLookup(dir, "")) }
}

func TestCompletionOffersSubcommandsAndTheirFlags(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`, withToolPages(t))
	c.notify("initialized", `{}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.sh", "tool \ntool stop -\ntool extra -\n"))

	var subcommands lsp.CompletionList
	deadline := time.Now().Add(5 * time.Second)
	for {
		subcommands = decodeResult[lsp.CompletionList](t, c.call("textDocument/completion", positionParams("file:///w/a.sh", 0, 5, "")))
		if !subcommands.IsIncomplete {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("subcommands still loading: %v", completionLabels(subcommands))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := completionLabels(subcommands); !slices.Equal(got, []string{"extra", "start", "stop"}) {
		t.Fatalf("subcommands = %v", got)
	}
	start := subcommands.Items[1]
	if start.Kind != lsp.CompletionMethod || start.Detail != "Start units." || start.Documentation == nil || !strings.Contains(start.Documentation.Value, "tool start UNIT...") {
		t.Errorf("start item = %+v", start)
	}
	stop := decodeResult[lsp.CompletionList](t, c.call("textDocument/completion", positionParams("file:///w/a.sh", 1, 11, "")))
	if got := completionLabels(stop); !slices.Equal(got, []string{"--force"}) {
		t.Errorf("tool stop flags = %v", got)
	}
	extra := decodeResult[lsp.CompletionList](t, c.call("textDocument/completion", positionParams("file:///w/a.sh", 2, 12, "")))
	if got := completionLabels(extra); !slices.Equal(got, []string{"-x"}) {
		t.Errorf("tool extra flags = %v", got)
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
