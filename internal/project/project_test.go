package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/editorconfig"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
)

func TestFromEditorConfig(t *testing.T) {
	t.Parallel()

	p, err := FromEditorConfig(editorconfig.Properties{
		"indent_style": "space", "indent_size": "4", "binary_next_line": "TRUE",
		"switch_case_indent": "false", "keep_padding": "true", "shell_variant": "posix", "ignore": "true",
		"end_of_line": "lf",
	})
	if err != nil {
		t.Fatal(err)
	}
	if *p.Style.Indent != 4 || !*p.Style.BinaryNextLine || *p.Style.SwitchCaseIndent || !*p.Style.KeepPadding ||
		p.Style.Minify != nil || *p.Dialect != dialect.Posix || !p.Ignore {
		t.Errorf("project settings = %+v", p)
	}
	for props, want := range map[string]uint{"tab|4": 0, "space|tab": 0, "|8": 8} {
		style, size, _ := strings.Cut(props, "|")
		p, err := FromEditorConfig(editorconfig.Properties{"indent_style": style, "indent_size": size})
		if err != nil || p.Style.Indent == nil || *p.Style.Indent != want {
			t.Errorf("indent %s = %v, %v; want %d", props, p.Style.Indent, err, want)
		}
	}
	if p, err := FromEditorConfig(editorconfig.Properties{"indent_style": "space"}); err != nil || p.Style.Indent != nil {
		t.Errorf("space without a size = %v, %v; want the editor's size", p.Style.Indent, err)
	}
	for _, bad := range []editorconfig.Properties{
		{"indent_size": "lots"}, {"indent_style": "wavy"}, {"simplify": "yes"}, {"shell_variant": "fish"}, {"ignore": "1"},
	} {
		if _, err := FromEditorConfig(bad); err == nil {
			t.Errorf("%v was accepted", bad)
		}
	}
}

func TestShellVariantRanksBelowTheForcedDialect(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, editorconfig.FileName), []byte("root = true\n[*]\nshell_variant = zsh\n[[zsh]]\nminify = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolver := editorconfig.NewResolver(os.ReadFile, time.Now, time.Second)
	script := filepath.Join(dir, "a.sh")
	in := dialect.Input{URI: fileuri.FromPath(script), Text: "#!/bin/bash\necho\n"}
	r, err := Resolve(resolver, script, nil, in)
	if err != nil || r.Dialect != dialect.Zsh || r.Source != dialect.SourceEditorConfig || r.Evidence != "zsh" {
		t.Errorf("resolution = %+v, %v; want zsh from shell_variant", r, err)
	}
	if r.Settings.Style.Minify != nil {
		t.Error("a [[zsh]] section applied to a script detected as bash")
	}
	forced := dialect.Ksh
	if r, _ := Resolve(resolver, script, &forced, in); r.Dialect != dialect.Ksh {
		t.Errorf("dialect = %v; the forced dialect must win", r.Dialect)
	}
}

func TestResolveReportsWhetherAnythingNamedTheShell(t *testing.T) {
	t.Parallel()

	resolver := editorconfig.NewResolver(os.ReadFile, time.Now, time.Second)
	dir := t.TempDir()
	for name, want := range map[string]bool{"script": false, "script.bash": true} {
		path := filepath.Join(dir, name)
		r, err := Resolve(resolver, path, nil, dialect.Input{URI: fileuri.FromPath(path), Text: "echo hi\n"})
		if err != nil || r.Named() != want || r.Dialect != dialect.Bash {
			t.Errorf("%s: resolution = %+v, %v; want named=%v", name, r, err, want)
		}
	}
}
