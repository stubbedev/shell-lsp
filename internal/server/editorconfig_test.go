package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/editorconfig"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

func TestReadProjectFormat(t *testing.T) {
	t.Parallel()

	p, err := readProjectFormat(editorconfig.Properties{
		"indent_style": "space", "indent_size": "4", "binary_next_line": "TRUE",
		"switch_case_indent": "false", "keep_padding": "true", "shell_variant": "posix", "ignore": "true",
		"end_of_line": "lf",
	})
	if err != nil {
		t.Fatal(err)
	}
	if *p.style.Indent != 4 || !*p.style.BinaryNextLine || *p.style.SwitchCaseIndent || !*p.style.KeepPadding ||
		p.style.Minify != nil || *p.dialect != dialect.Posix || !p.ignore {
		t.Errorf("project format = %+v", p)
	}
	for props, want := range map[string]uint{"tab|4": 0, "space|tab": 0, "|8": 8} {
		style, size, _ := strings.Cut(props, "|")
		p, err := readProjectFormat(editorconfig.Properties{"indent_style": style, "indent_size": size})
		if err != nil || p.style.Indent == nil || *p.style.Indent != want {
			t.Errorf("indent %s = %v, %v; want %d", props, p.style.Indent, err, want)
		}
	}
	if p, err := readProjectFormat(editorconfig.Properties{"indent_style": "space"}); err != nil || p.style.Indent != nil {
		t.Errorf("space without a size = %v, %v; want the editor's size", p.style.Indent, err)
	}
	for _, bad := range []editorconfig.Properties{
		{"indent_size": "lots"}, {"indent_style": "wavy"}, {"simplify": "yes"}, {"shell_variant": "fish"}, {"ignore": "1"},
	} {
		if _, err := readProjectFormat(bad); err == nil {
			t.Errorf("%v was accepted", bad)
		}
	}
}

func TestEditorConfigShellVariantRanksBelowTheDialectSetting(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, editorconfig.FileName), "root = true\n[*]\nshell_variant = zsh\n[[zsh]]\nminify = true\n")
	s := &Server{editorconfig: editorconfig.NewResolver(os.ReadFile, time.Now, workspace.DiskRecheck)}
	in := dialect.Input{URI: fileuri.FromPath(filepath.Join(dir, "a.sh")), Text: "#!/bin/bash\necho\n"}
	d, project, err := s.resolveDialect(filepath.Join(dir, "a.sh"), nil, in)
	if err != nil || d != dialect.Zsh {
		t.Errorf("dialect = %v, %v; want zsh from shell_variant", d, err)
	}
	if project.style.Minify != nil {
		t.Error("a [[zsh]] section applied to a script detected as bash")
	}
	forced := dialect.Ksh
	if d, _, _ := s.resolveDialect(filepath.Join(dir, "a.sh"), &forced, in); d != dialect.Ksh {
		t.Errorf("dialect = %v; the dialect setting must win", d)
	}
}

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
