package hostnav

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/host/hosttest"
	"github.com/stubbedev/shell-lsp/internal/host/justfile"
	"github.com/stubbedev/shell-lsp/internal/host/oracle"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

var update = flag.Bool("update", false, "record the reference tools' verdicts and the current findings in the golden files")

func newWorkspace() *workspace.Workspace {
	return workspace.New(func(string) (*workspace.File, bool) { return nil, false }, workspace.Detect)
}

func groupFor(t *testing.T, ws *workspace.Workspace, path string, files navigation.WorkspaceFiles) *Group {
	t.Helper()
	file, err := ws.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	g, err := Build(context.Background(), hostOf(t, file), envWith(ws, hostOf(t, file).Language(), files))
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func envWith(loader Loader, language host.Language, files navigation.WorkspaceFiles) Env {
	env := Env{Loader: loader}
	if files != nil {
		env.Graph = func(ctx context.Context) (*Graph, error) { return NewGraph(ctx, language, files, env) }
	}
	return env
}

func hostOf(t *testing.T, file *workspace.File) Host {
	t.Helper()
	h, ok := Of(file)
	if !ok {
		t.Fatalf("%s is not a justfile or Makefile", file.Path)
	}
	return h
}

func writeTree(t testing.TB, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func findingsAlone(t *testing.T, name string) hosttest.Analyze {
	return func(text string) []lint.Finding {
		dir := writeTree(t, map[string]string{name: text})
		return groupFor(t, newWorkspace(), filepath.Join(dir, name), nil).Findings()
	}
}

func TestJustChecksAgreeWithJust(t *testing.T) {
	t.Parallel()

	if *update {
		hosttest.RequireTool(t, "just", "just "+justfile.Version)
	}
	analyze := findingsAlone(t, "justfile")
	hosttest.Check(t, filepath.Join("testdata", "check", "just.txt"), analyze, hosttest.JustVerdict, justReports(analyze), *update)
}

const justOracleSystem = "linux"

var justParsePhase = []lint.Code{
	lint.CodeJustSyntax, lint.CodeJustUnknownAttribute, lint.CodeJustAttributeArguments, lint.CodeJustNeedsSetting,
	lint.CodeJustUnknownSetting, lint.CodeJustSettingValue,
}

func justReports(analyze hosttest.Analyze) hosttest.Reports {
	return func(text string, _ oracle.Verdict, findings []lint.Finding) []lint.Finding {
		enabled, blanked := enabledOnly(text)
		if !blanked {
			return findings
		}
		analysis := func(f lint.Finding) bool { return !slices.Contains(justParsePhase, f.Code) }
		out := slices.DeleteFunc(slices.Clone(findings), analysis)
		for _, f := range analyze(enabled) {
			if analysis(f) {
				out = append(out, f)
			}
		}
		return out
	}
}

func enabledOnly(text string) (string, bool) {
	out := []byte(text)
	blanked := false
	for _, it := range justfile.Parse(text).Items {
		if it.EnabledOn(justOracleSystem) {
			continue
		}
		blanked = true
		start := it.Span().Start
		for _, a := range it.Attributes() {
			start = min(start, a.Bracket)
		}
		end := it.Span().End
		for end < len(out) && out[end] != '\n' {
			end++
		}
		for i := start; i < end; i++ {
			if out[i] != '\n' {
				out[i] = ' '
			}
		}
	}
	return string(out), blanked
}

func TestMakeChecksAgreeWithMake(t *testing.T) {
	t.Parallel()

	if *update {
		hosttest.RequireTool(t, "make", hosttest.MakeVersion)
	}
	hosttest.Check(t, filepath.Join("testdata", "check", "make.txt"), findingsAlone(t, "Makefile"), hosttest.MakeVerdict, makeReports, *update)
}

func TestNixSyntaxErrorsAreFindings(t *testing.T) {
	t.Parallel()

	findings := findingsAlone(t, "default.nix")("{ a = ; buildPhase = ''\n  make\n''; }\n")
	if len(findings) != 1 || findings[0].Code != lint.CodeNixSyntax || findings[0].Span.Start != 6 {
		t.Errorf("findings = %+v", findings)
	}
	if clean := findingsAlone(t, "default.nix")("{ a = 1; }\n"); len(clean) != 0 {
		t.Errorf("a valid file reports %+v", clean)
	}
}

func TestNixChecksAreFindings(t *testing.T) {
	t.Parallel()

	findings := findingsAlone(t, "default.nix")("# shellcheck disable=SL8005\nlet a = 1; in b\n")
	if len(findings) != 1 || findings[0].Code != lint.CodeNixUndefinedVariable || findings[0].Severity != lint.SeverityError {
		t.Errorf("findings = %+v", findings)
	}
}
