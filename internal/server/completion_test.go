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
	"github.com/stubbedev/shell-lsp/internal/platform"
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
	if greeting.TextEdit == nil || greeting.TextEdit.Insert != want || greeting.Kind != lsp.CompletionVariable {
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

func TestCompletionTriggeredInADirective(t *testing.T) {
	t.Parallel()

	uri := fileuri.FromPath(filepath.Join(t.TempDir(), "main.sh"))
	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openParams(uri, "# shellcheck disable=\nx=\n"))
	triggered := func(line, character int) lsp.CompletionList {
		t.Helper()
		params, err := json.Marshal(lsp.CompletionParams{
			TextDocument: lsp.TextDocumentIdentifier{URI: uri},
			Position:     lsp.Position{Line: uint32(line), Character: uint32(character)},
			Context:      &lsp.CompletionContext{TriggerKind: 2, TriggerCharacter: "="},
		})
		if err != nil {
			t.Fatal(err)
		}
		return decodeResult[lsp.CompletionList](t, c.call("textDocument/completion", string(params)))
	}
	if codes := triggered(0, 21); !slices.Contains(completionLabels(codes), "SC2086") {
		t.Errorf("= in a disable directive offered %d items without SC2086", len(codes.Items))
	}
	if assignment := triggered(1, 2); len(assignment.Items) != 0 {
		t.Errorf("= after an assignment offered %v", completionLabels(assignment))
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
	return withPages(t, map[string]string{"tool": toolPage, "tool-extra": toolExtraPage})
}

func withPages(t testing.TB, pages map[string]string) func(*Server) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "man1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, page := range pages {
		if err := os.WriteFile(filepath.Join(dir, name+".1"), []byte(page), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return func(s *Server) { s.usePages(manpage.NewLookup(filepath.Dir(dir), "")) }
}

func stubPage(name string) string {
	return ".TH " + strings.ToUpper(name) + " 1\n.SH NAME\n" + name + " \\- a test command\n"
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

const (
	boxPage = `.TH BOX 1
.SH NAME
box \- keep things in boxes
.SH SYNOPSIS
.B box
[\fB\-C\fR \fIdir\fR] \fICOMMAND\fR
.SH OPTIONS
.TP
\fB\-C\fR \fIdir\fR
run in dir
.SH COMMANDS
.TP
.BR box-remote (1)
manage remotes
`
	boxRemotePage = `.TH BOX-REMOTE 1
.SH NAME
box-remote \- manage remote boxes
.SH SYNOPSIS
.B box remote add
\fIname\fR
.br
.B box remote rename
\fIold\fR \fInew\fR
.SH COMMANDS
.TP
.B add \fIname\fR
Add a remote box.
.TP
.B rename \fIold\fR \fInew\fR
Rename a remote box.
`
)

func TestCompletionAndHoverFollowNestedSubcommands(t *testing.T) {
	t.Parallel()

	c, _ := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`, withPages(t, map[string]string{"box": boxPage, "box-remote": boxRemotePage}))
	c.notify("initialized", `{}`)
	c.notify("textDocument/didOpen", openParams("file:///w/a.sh", "box -C remote remote \nbox -C dir remote add origin\nsudo -E box remote \nsudo -u root box -C dir remote add origin\n"))

	var nested lsp.CompletionList
	deadline := time.Now().Add(5 * time.Second)
	for {
		nested = decodeResult[lsp.CompletionList](t, c.call("textDocument/completion", positionParams("file:///w/a.sh", 0, 21, "")))
		if !nested.IsIncomplete {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("subcommands still loading: %v", completionLabels(nested))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := completionLabels(nested); !slices.Equal(got, []string{"add", "rename"}) {
		t.Fatalf("box remote subcommands = %v", got)
	}
	hover := decodeResult[lsp.Hover](t, c.call("textDocument/hover", positionParams("file:///w/a.sh", 1, 19, "")))
	if !strings.Contains(hover.Contents.Value, "box remote add name") || !strings.Contains(hover.Contents.Value, "Add a remote box.") {
		t.Errorf("hover over box remote add = %q", hover.Contents.Value)
	}
	wrapped := decodeResult[lsp.CompletionList](t, c.call("textDocument/completion", positionParams("file:///w/a.sh", 2, 19, "")))
	if got := completionLabels(wrapped); !slices.Equal(got, []string{"add", "rename"}) {
		t.Errorf("box remote subcommands behind sudo = %v", got)
	}
	hover = decodeResult[lsp.Hover](t, c.call("textDocument/hover", positionParams("file:///w/a.sh", 3, 32, "")))
	if !strings.Contains(hover.Contents.Value, "box remote add name") {
		t.Errorf("hover over box remote add behind sudo = %q", hover.Contents.Value)
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

func executableName(name string) string {
	if platform.Native == platform.Windows {
		return name + ".exe"
	}
	return name
}

func TestExecutableCache(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for name, mode := range map[string]os.FileMode{executableName("tool"): 0o755, "data.txt": 0o644} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, executableName("subdir")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, executableName("tool")), filepath.Join(dir, executableName("linked"))); err != nil {
		t.Fatal(err)
	}
	cache := newExecutableCache([]string{dir, filepath.Join(dir, "missing")}, platform.Native.PathExt(""))
	start := time.Now()
	if got := cache.list(start); !slices.Equal(got, []string{"linked", "tool"}) {
		t.Errorf("executables = %v", got)
	}
	if err := os.WriteFile(filepath.Join(dir, executableName("newtool")), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	later := start.Add(time.Second)
	if err := os.Chtimes(dir, later, later); err != nil {
		t.Fatal(err)
	}
	if got := cache.list(start.Add(workspace.DiskRecheck / 2)); !slices.Equal(got, []string{"linked", "tool"}) {
		t.Errorf("executables within the recheck interval = %v, want the cached list", got)
	}
	if got := cache.list(start.Add(workspace.DiskRecheck)); !slices.Equal(got, []string{"linked", "newtool", "tool"}) {
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
