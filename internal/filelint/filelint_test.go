package filelint

import (
	"os"
	"path/filepath"
	"slices"
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

func TestCheckFollowsSourcedFilesAndHonoursOptions(t *testing.T) {
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
	ws := workspace.New(func(string) (*workspace.File, bool) { return nil, false }, workspace.DetectDialect)
	file, err := ws.Load(filepath.Join(dir, "main.sh"))
	if err != nil {
		t.Fatal(err)
	}
	env := navigation.Env{Loader: loader{ws}, Roots: []string{dir}}
	got := codes(Check(file, env, Options{}).Findings)
	if !slices.Equal(got, []lint.Code{2086}) {
		t.Errorf("findings = %v; the sourced file defines LIBVAR, so only SC2086 remains", got)
	}
	if got := codes(Check(file, env, Options{Exclude: lint.CodesOf(2086)}).Findings); len(got) != 0 {
		t.Errorf("Exclude kept %v", got)
	}
	if got := codes(Check(file, env, Options{Optional: []string{"require-variable-braces"}}).Findings); !slices.Contains(got, 2250) {
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
	ws := workspace.New(func(string) (*workspace.File, bool) { return nil, false }, workspace.DetectDialect)
	file, err := ws.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	var ends []int
	for _, f := range Check(file, navigation.Env{Loader: loader{ws}, Roots: []string{dir}}, Options{}).Findings {
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
	ws := workspace.New(func(string) (*workspace.File, bool) { return nil, false }, workspace.DetectDialect)
	file, err := ws.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := codes(Check(file, navigation.Env{Loader: loader{ws}, Roots: []string{dir}}, Options{}).Findings); !slices.Contains(got, 1082) {
		t.Errorf("findings = %v, want SC1082 for the byte order mark the store strips", got)
	}
}
