//go:build unix

package server

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/editorconfig"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
)

func TestSourcedSpecialFilesAreNeitherWaitedOnNorRead(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "sub")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pipe.sh", editorconfig.FileName} {
		if err := syscall.Mkfifo(filepath.Join(dir, name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	uri := fileuri.FromPath(filepath.Join(dir, "main.sh"))
	c, _ := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", "capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("initialized", `{}`)
	c.notify("textDocument/didOpen", openParams(uri, "#!/bin/bash\nsource ./pipe.sh\nsource /dev/zero\nsource /dev/stdin <<< 'echo hi'\n"))
	c.call("textDocument/diagnostic", `{"textDocument": {"uri": "`+uri+`"}}`)
	c.call("textDocument/definition", positionParams(uri, 1, 9, ""))
	c.call("workspace/symbol", `{"query": ""}`)
}
