package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/editorconfig"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

func writeTestFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

const caseScript = "case $1 in\na) echo a ;;\nesac\nf() {\necho x\n}\n"

func formatted(t *testing.T, c *testClient, uri string) string {
	t.Helper()
	edits := decodeResult[[]lsp.TextEdit](t, c.call("textDocument/formatting",
		fmt.Sprintf(`{"textDocument": {"uri": %q}, "options": {"tabSize": 2, "insertSpaces": true}}`, uri)))
	var out strings.Builder
	for _, e := range edits {
		out.WriteString(e.NewText)
	}
	return out.String()
}

func TestFormattingFollowsEditorConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	config := filepath.Join(dir, editorconfig.FileName)
	writeTestFile(t, config, "root = true\n[*.sh]\nindent_style = space\nindent_size = 4\nswitch_case_indent = true\n[vendor/**]\nignore = true\n")
	c, _ := startClient(t, fmt.Sprintf(`{"rootUri": %q, "capabilities": {"workspace": {"didChangeWatchedFiles": {"dynamicRegistration": true}}}}`, fileuri.FromPath(dir)))
	script := fileuri.FromPath(filepath.Join(dir, "a.sh"))
	c.notify("textDocument/didOpen", openParams(script, caseScript))
	if got := formatted(t, c, script); !strings.Contains(got, "    a) echo a ;;") || !strings.Contains(got, "    echo x") {
		t.Errorf("formatting ignored .editorconfig:\n%s", got)
	}

	vendored := fileuri.FromPath(filepath.Join(dir, "vendor", "lib.sh"))
	c.notify("textDocument/didOpen", openParams(vendored, caseScript))
	if got := formatted(t, c, vendored); got != "" {
		t.Errorf("an ignored file was formatted:\n%s", got)
	}

	writeTestFile(t, config, "root = true\n[*.sh]\nindent_size = 3\n")
	c.notify("workspace/didChangeWatchedFiles", fmt.Sprintf(`{"changes": [{"uri": %q, "type": 2}]}`, fileuri.FromPath(config)))
	if got := formatted(t, c, script); !strings.Contains(got, "   echo x") || strings.Contains(got, "   a) echo a") {
		t.Errorf("formatting kept the old .editorconfig after it changed:\n%s", got)
	}

	writeTestFile(t, config, "root = true\n[*.sh]\nindent_size = lots\n")
	c.notify("workspace/didChangeWatchedFiles", fmt.Sprintf(`{"changes": [{"uri": %q, "type": 2}]}`, fileuri.FromPath(config)))
	if msg := c.call("textDocument/formatting", fmt.Sprintf(`{"textDocument": {"uri": %q}, "options": {"tabSize": 2, "insertSpaces": true}}`, script)); msg.Error == nil || !strings.Contains(msg.Error.Message, "indent_size = lots") {
		t.Errorf("a malformed .editorconfig did not fail formatting: %+v", msg.Error)
	}
}

func TestLSPFormatSettingsBeatEditorConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, editorconfig.FileName), "root = true\n[*]\nindent_size = 4\n")
	c, _ := startClient(t, fmt.Sprintf(`{"rootUri": %q, "capabilities": {}, "initializationOptions": {"format": {"indent": 1}}}`, fileuri.FromPath(dir)))
	script := fileuri.FromPath(filepath.Join(dir, "a.sh"))
	c.notify("textDocument/didOpen", openParams(script, caseScript))
	if got := formatted(t, c, script); !strings.Contains(got, " echo x") || strings.Contains(got, "  echo x") {
		t.Errorf("format.indent did not win over .editorconfig:\n%s", got)
	}
}
