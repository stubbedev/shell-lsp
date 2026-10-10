package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

func TestSourcedFilesActAsImports(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	lib := filepath.Join(root, "lib", "common.sh")
	if err := os.MkdirAll(filepath.Dir(lib), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lib, []byte("# Greets someone.\ngreet() { echo \"hi $1\"; }\n# Who to greet.\nLIB_NAME=world\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mainURI := fileuri.FromPath(filepath.Join(root, "main.sh"))
	c, _ := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", "capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openParams(mainURI, "source ./lib/common.sh\ngreet \"$LIB_NAME\"\ngr\necho \"$LI\"\n"))

	hover := func(line, character int) string {
		t.Helper()
		return decodeResult[lsp.Hover](t, c.call("textDocument/hover", positionParams(mainURI, line, character, ""))).Contents.Value
	}
	if got := hover(1, 2); !strings.Contains(got, "Greets someone.") || !strings.Contains(got, "From `lib/common.sh`.") {
		t.Errorf("hover on imported function = %q", got)
	}
	if got := hover(1, 10); !strings.Contains(got, "LIB_NAME=world") || !strings.Contains(got, "From `lib/common.sh`.") {
		t.Errorf("hover on imported global = %q", got)
	}

	complete := func(line, character int) lsp.CompletionList {
		t.Helper()
		return decodeResult[lsp.CompletionList](t, c.call("textDocument/completion", positionParams(mainURI, line, character, "")))
	}
	detail := func(list lsp.CompletionList, label string) string {
		i := slices.IndexFunc(list.Items, func(it lsp.CompletionItem) bool { return it.Label == label })
		if i < 0 {
			return ""
		}
		return list.Items[i].Detail
	}
	commands := complete(2, 2)
	if got := detail(commands, "greet"); got != "function from lib/common.sh" {
		t.Errorf("greet completion detail = %q in %v", got, completionLabels(commands))
	}
	i := slices.IndexFunc(commands.Items, func(it lsp.CompletionItem) bool { return it.Label == "greet" })
	if i < 0 {
		t.Fatalf("greet missing from %v", completionLabels(commands))
	}
	item, err := json.Marshal(commands.Items[i])
	if err != nil {
		t.Fatal(err)
	}
	resolved := decodeResult[lsp.CompletionItem](t, c.call("completionItem/resolve", string(item)))
	if resolved.Documentation == nil || !strings.Contains(resolved.Documentation.Value, "Greets someone.") {
		t.Errorf("resolved greet documentation = %+v", resolved.Documentation)
	}
	if got := detail(complete(3, 9), "LIB_NAME"); got != "global from lib/common.sh" {
		t.Errorf("LIB_NAME completion detail = %q", got)
	}

	c.notify("textDocument/didOpen", openParams(fileuri.FromPath(lib), "greet_loudly() { :; }\n"))
	edited := complete(2, 2)
	if detail(edited, "greet_loudly") == "" || detail(edited, "greet") != "" {
		t.Errorf("completion after editing the open import = %v", completionLabels(edited))
	}
}

func TestLintFollowsSourcedFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	lib := filepath.Join(root, "lib.sh")
	if err := os.WriteFile(lib, []byte("lib_name=world\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	uri := fileuri.FromPath(filepath.Join(root, "main.sh"))
	c, _ := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", "capabilities": {}}`)
	c.notify("textDocument/didOpen", openParams(uri, "#!/bin/bash\nsource ./lib.sh\necho $lib_name\n"))
	if got := awaitPublished(c, uri, 1); len(got.Diagnostics) != 0 {
		t.Errorf("diagnostics with a spaceless imported value = %v, want none", codes(got.Diagnostics))
	}

	if err := os.WriteFile(lib, []byte("lib_name='hello world'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.notify("workspace/didChangeWatchedFiles", `{"changes": [{"uri": "`+fileuri.FromPath(lib)+`", "type": 2}]}`)
	c.awaitNotification("textDocument/publishDiagnostics", func(raw json.RawMessage) bool {
		var p lsp.PublishDiagnosticsParams
		return json.Unmarshal(raw, &p) == nil && p.URI == uri && slices.Contains(codes(p.Diagnostics), "SC2086")
	})
}

func TestLintReportsUnfollowedAndBrokenSources(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "broken.sh"), []byte("for x in; do\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	uri := fileuri.FromPath(filepath.Join(root, "main.sh"))
	c, _ := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", "capabilities": {}}`)
	c.notify("textDocument/didOpen", openParams(uri, "#!/bin/bash\nsource -- ./broken.sh\nsource ./missing.sh\n"))
	if got := codes(awaitPublished(c, uri, 1).Diagnostics); !slices.Equal(got, []string{"SC1091", "SC1094"}) {
		t.Errorf("diagnostics = %v, want SC1091 and SC1094", got)
	}
}
