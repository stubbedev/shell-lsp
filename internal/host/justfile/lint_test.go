package justfile_test

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/filelint"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

type noFiles struct{}

func (noFiles) Load(path string) (*workspace.File, error) {
	return nil, errors.New("no file to load at " + path)
}

func (noFiles) Generation() uint64 { return 0 }

func TestShellFlagsReachErrexitChecks(t *testing.T) {
	t.Parallel()

	const recipe = "x:\n    cd x\n    ls\n"
	for _, tt := range []struct {
		name, settings string
		reported       bool
	}{
		{"default shell", "", true},
		{"shell without errexit", "set shell := [\"bash\", \"-uc\"]\n", true},
		{"shell with errexit", "set shell := [\"bash\", \"-euo\", \"pipefail\", \"-c\"]\n", false},
		{"shell with errexit behind env", "set shell := [\"/usr/bin/env\", \"bash\", \"-e\", \"-c\"]\n", false},
		{"windows shell with errexit", "set windows-shell := [\"bash\", \"-ec\"]\n", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "justfile")
			file := workspace.Parse(path, docstore.NewDocument(fileuri.FromPath(path), "just", 1, tt.settings+recipe), nil, nil)
			report := filelint.Check(file, navigation.Env{Loader: noFiles{}}, filelint.Settings{})
			reported := slices.ContainsFunc(report.Findings, func(f lint.Finding) bool { return f.Code == 2164 })
			if reported != tt.reported {
				t.Errorf("SC2164 reported = %v, want %v; findings %v", reported, tt.reported, report.Findings)
			}
		})
	}
}
