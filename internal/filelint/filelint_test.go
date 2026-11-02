package filelint

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

type loader struct {
	ws *workspace.Workspace
}

func (l loader) Load(path string) (*workspace.File, error) { return l.ws.Load(path) }

func (loader) Generation() uint64 { return 0 }

func codes(findings []lint.Finding) []lint.Code {
	out := make([]lint.Code, len(findings))
	for i, f := range findings {
		out[i] = f.Code
	}
	return out
}

func TestCheckFollowsSourcedFilesAndHonoursSettings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for name, text := range map[string]string{
		"main.sh": "#!/bin/bash\nsource ./lib.sh\necho \"$LIBVAR\" $1\n",
		"lib.sh":  "#!/bin/bash\nLIBVAR=1\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ws := workspace.New(func(string) (*workspace.File, bool) { return nil, false }, workspace.Detect)
	file, err := ws.Load(filepath.Join(dir, "main.sh"))
	if err != nil {
		t.Fatal(err)
	}
	env := navigation.Env{Loader: loader{ws}, Roots: []string{dir}}
	got := codes(Check(file, env, Settings{}).Findings)
	if !slices.Equal(got, []lint.Code{2086}) {
		t.Errorf("findings = %v; the sourced file defines LIBVAR, so only SC2086 remains", got)
	}
	if got := codes(Check(file, env, Settings{Exclude: lint.CodesOf(2086)}).Findings); len(got) != 0 {
		t.Errorf("Exclude kept %v", got)
	}
	if got := codes(Check(file, env, Settings{Optional: []string{"require-variable-braces"}}).Findings); !slices.Contains(got, 2250) {
		t.Errorf("an optional check was not enabled: %v", got)
	}
}

func TestCheckReportsWindowsLineEndings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "crlf.sh")
	if err := os.WriteFile(path, []byte("#!/bin/bash\r\necho hi\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ws := workspace.New(func(string) (*workspace.File, bool) { return nil, false }, workspace.Detect)
	file, err := ws.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	var ends []int
	for _, f := range Check(file, navigation.Env{Loader: loader{ws}, Roots: []string{dir}}, Settings{}).Findings {
		if f.Code == 1017 {
			ends = append(ends, f.Span.Start)
		}
	}
	if want := []int{11, 19}; !slices.Equal(ends, want) {
		t.Errorf("SC1017 at %v, want the end of each CRLF line %v", ends, want)
	}
}

func TestCheckReportsAByteOrderMark(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "bom.sh")
	if err := os.WriteFile(path, []byte("\uFEFF#!/bin/bash\necho hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ws := workspace.New(func(string) (*workspace.File, bool) { return nil, false }, workspace.Detect)
	file, err := ws.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := codes(Check(file, navigation.Env{Loader: loader{ws}, Roots: []string{dir}}, Settings{}).Findings); !slices.Contains(got, 1082) {
		t.Errorf("findings = %v, want SC1082 for the byte order mark the store strips", got)
	}
}

func TestCheckLintsEachRecipeAsItsOwnShell(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	files := map[string]string{
		"justfile": "plain:\n    arr=(1 2)\nbash:\n    #!/usr/bin/env bash\n    arr=(1 2)\n",
		"Makefile": "plain:\n\tarr=(1 2)\nbash: SHELL := /bin/bash\nbash:\n\tarr=(1 2)\n",
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ws := workspace.New(func(string) (*workspace.File, bool) { return nil, false }, workspace.Detect)
	for name, text := range files {
		file, err := ws.Load(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		var lines []int
		for _, f := range Check(file, navigation.Env{Loader: loader{ws}, Roots: []string{dir}}, Settings{}).Findings {
			if f.Code == 3030 {
				lines = append(lines, strings.Count(text[:file.Doc.SourceOffset(f.Span.Start)], "\n")+1)
			}
		}
		if !slices.Equal(lines, []int{2}) {
			t.Errorf("%s: arrays reported on lines %v, want only line 2, the recipe sh runs", name, lines)
		}
	}
}

func TestNixStringsReportOnlyWhatTheShellWouldDo(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, nix string
		want      []lint.Code
	}{
		{"a store path needs no quotes", "{ installPhase = ''\n  mkdir -p $out/bin\n''; }", nil},
		{"nor a name built from one", "{ installPhase = ''\n  d=$out/bin\n  mkdir -p $d\n''; }", nil},
		{"a declared output", "{ outputs = [ \"out\" \"dev\" ]; installPhase = ''\n  mkdir -p $dev\n''; }", nil},
		{"an undeclared variable stays reported", "{ installPhase = ''\n  mkdir -p $custom\n''; }", []lint.Code{2086}},
		{"$out is not a store path in a dev shell", "{ shellHook = ''\n  mkdir -p $out\n''; }", []lint.Code{2086}},
		{"or in a plain script", "writeShellScript \"a\" ''\n  out=\"a b\"\n  mkdir -p $out\n''", []lint.Code{2086}},
		{"Home Manager's deliberate word splitting", "{ home.activation.a = ''\n  $DRY_RUN_CMD mkdir $VERBOSE_ARG x\n''; }", nil},
		{"the same names elsewhere are not provided", "{ programs.bash.initExtra = ''\n  mkdir $VERBOSE_ARG x\n''; }", []lint.Code{2086}},
		{"a sourced function is for later", "{ programs.zsh.initContent = ''\n  hop() { echo hi; }\n''; }", nil},
		{"a sourced variable is for later", "{ programs.zsh.initContent = ''\n  HOP=1\n''; }", nil},
		{"a session variable is not this fragment's to assign", "{ programs.bash.initExtra = ''\n  echo \"$session_var\"\n''; }", nil},
		{"a dev shell hook is sourced too", "{ shellHook = ''\n  hop() { echo hi; }\n''; }", nil},
		{"a marked fragment says nothing about how it runs", "{ a = /* zsh */ ''\n  hop() { echo hi; }\n  echo \"$session_var\"\n''; }", nil},
		{"a hole is a literal in an alias body", "{ programs.zsh.initContent = ''\n  alias a='${lib.getExe' pkgs.gzip \"gzip\"} -dc'\n''; }", nil},
		{"a hole is a literal in a case pattern", "{ programs.zsh.initContent = ''\n  case $1 in\n    ${verbs}) echo hi ;;\n  esac\n''; }", nil},
		{"a store path cannot be followed", "{ programs.zsh.initContent = ''\n  source ${pkgs.a}/b\n  source ${c}\n''; }", nil},
		{"nor a run-time path", "{ programs.zsh.initContent = ''\n  f=\"$HOME/x\"\n  source \"$f\"\n''; }", nil},
		{"a real mistake is still found", "{ programs.bash.initExtra = ''\n  rm -rf $1\n''; }", []lint.Code{2086}},
		{"a cd that ends a sourced function leaves no command to go wrong", "{ programs.zsh.initContent = ''\n  hop() { cd \"$1\"; }\n''; }", nil},
		{"nor does one before a plain return", "{ programs.zsh.initContent = ''\n  hop() {\n    if [ -n \"$1\" ]; then\n      cd \"$1\"\n      return\n    fi\n    echo none\n  }\n''; }", nil},
		{"a sourced cd with a command after it is still found", "{ programs.bash.initExtra = ''\n  hop() { cd \"$1\"; ls; }\n''; }", []lint.Code{2164}},
		{"a sourced function the fragment calls is still found", "{ programs.bash.initExtra = ''\n  hop() { cd \"$1\"; }\n  hop /tmp\n  rm -f ./x\n''; }", []lint.Code{2164}},
		{"a plain script keeps ShellCheck's verdict on the same function", "writeShellScript \"a\" ''\n  hop() { cd \"$1\"; }\n''", []lint.Code{2329, 2164}},
		{"a sourced function's local cannot exist on the server", "{ programs.bash.initExtra = ''\n  f() {\n    local value\n    value=$(date)\n    ssh host \"echo $value\"\n  }\n''; }", nil},
		{"a sourced variable that is no local expands on the client", "{ programs.bash.initExtra = ''\n  value=1\n  ssh host \"echo $value\"\n''; }", []lint.Code{2029}},
		{"a plain script keeps ShellCheck's verdict on the same local", "writeShellScript \"a\" ''\n  f() {\n    local value\n    value=$(date)\n    ssh host \"echo $value\"\n  }\n  f\n''", []lint.Code{2029}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "default.nix")
			if err := os.WriteFile(path, []byte(tt.nix+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			ws := workspace.New(func(string) (*workspace.File, bool) { return nil, false }, workspace.Detect)
			file, err := ws.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			got := codes(Check(file, navigation.Env{Loader: loader{ws}, Roots: []string{dir}}, Settings{}).Findings)
			if !slices.Equal(got, append([]lint.Code{}, tt.want...)) {
				t.Errorf("%q reports %v, want %v", tt.nix, got, tt.want)
			}
		})
	}
}
