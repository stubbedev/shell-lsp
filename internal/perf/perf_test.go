package perf

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/actions"
	"github.com/stubbedev/shell-lsp/internal/assist"
	"github.com/stubbedev/shell-lsp/internal/completion"
	"github.com/stubbedev/shell-lsp/internal/diagnostics"
	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/format"
	"github.com/stubbedev/shell-lsp/internal/hover"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/manpage"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/semantic"
	"github.com/stubbedev/shell-lsp/internal/structure"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

type offlineEnv struct{}

func (offlineEnv) Executables() []string {
	names := make([]string, 3000)
	for i := range names {
		names[i] = fmt.Sprintf("cmd%04d", i)
	}
	return names
}
func (offlineEnv) ReadDir(string) ([]completion.DirEntry, error) { return nil, os.ErrNotExist }
func (offlineEnv) Home() string                                  { return "/home/u" }
func (offlineEnv) EnvironmentNames() []string                    { return []string{"HOME", "PATH"} }
func (offlineEnv) Options(context.Context, string) ([]manpage.Option, error) {
	return nil, manpage.ErrNoPage
}

type offlineCommands struct{}

func (offlineCommands) Describe(context.Context, string) (string, error) { return "external", nil }

var text = LargeScript(10000)

func document() *docstore.Document {
	return docstore.NewDocument("file:///large.bash", "shellscript", 1, text)
}

func BenchmarkParse(b *testing.B) {
	for b.Loop() {
		parser.Parse(text, dialect.Bash)
	}
}

func BenchmarkSyntaxDiagnostics(b *testing.B) {
	doc := document()
	for b.Loop() {
		diagnostics.Syntax(parser.Parse(text, dialect.Bash), doc, docstore.EncodingUTF16)
	}
}

func BenchmarkLint(b *testing.B) {
	tree := parser.Parse(text, dialect.Bash)
	for b.Loop() {
		lint.Check(tree, nil, lint.Options{})
	}
}

func BenchmarkCompletion(b *testing.B) {
	tree := parser.Parse(text, dialect.Bash)
	offset := strings.LastIndex(text, `"${items[@]}"`) + 4
	for b.Loop() {
		if _, err := completion.Complete(b.Context(), completion.Input{Tree: tree, Offset: offset, Env: offlineEnv{}}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHover(b *testing.B) {
	tree := parser.Parse(text, dialect.Bash)
	offset := strings.LastIndex(text, "$count")
	for b.Loop() {
		if _, _, err := hover.Hover(b.Context(), hover.Input{Tree: tree, Offset: offset, Commands: offlineCommands{}}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSemanticTokens(b *testing.B) {
	tree := parser.Parse(text, dialect.Bash)
	for b.Loop() {
		semantic.Tokens(tree, nil)
	}
}

func BenchmarkSymbols(b *testing.B) {
	tree := parser.Parse(text, dialect.Bash)
	for b.Loop() {
		structure.Symbols(tree)
	}
}

const (
	budgetRuns       = 21
	completionBudget = 50 * time.Millisecond
	diagnosticBudget = 200 * time.Millisecond
)

func p95(t *testing.T, run func()) time.Duration {
	t.Helper()

	samples := make([]time.Duration, budgetRuns)
	for i := range samples {
		start := time.Now()
		run()
		samples[i] = time.Since(start)
	}
	slices.Sort(samples)
	return samples[len(samples)*95/100]
}

func TestBudgets(t *testing.T) {
	if testing.Short() {
		t.Skip("budgets are measured in full test runs")
	}
	doc := document()
	diagnose := p95(t, func() { diagnostics.Syntax(parser.Parse(text, dialect.Bash), doc, docstore.EncodingUTF16) })
	if diagnose > diagnosticBudget {
		t.Errorf("parse + syntax diagnostics p95 = %v over 10k lines, budget %v", diagnose, diagnosticBudget)
	}
	tree := parser.Parse(text, dialect.Bash)
	offsets := []int{strings.LastIndex(text, `"${items[@]}"`) + 4, strings.LastIndex(text, "\tif ") + 1, strings.LastIndex(text, "grep ") + 2}
	for _, offset := range offsets {
		complete := p95(t, func() {
			if _, err := completion.Complete(t.Context(), completion.Input{Tree: tree, Offset: offset, Env: offlineEnv{}}); err != nil {
				t.Fatal(err)
			}
		})
		if complete > completionBudget {
			t.Errorf("completion at %d p95 = %v over 10k lines, budget %v", offset, complete, completionBudget)
		}
	}
	t.Logf("p95: diagnostics %v", diagnose)
}

func benchFile() *workspace.File {
	doc := document()
	return &workspace.File{Path: "/w/large.bash", URI: doc.URI, Doc: doc, Tree: parser.Parse(text, dialect.Bash)}
}

func benchEnv() navigation.Env {
	ws := workspace.New(func(string) (*workspace.File, bool) { return nil, false }, func(string) *dialect.Dialect { return nil })
	return navigation.Env{Loader: ws}
}

func BenchmarkFolds(b *testing.B) {
	file := benchFile()
	for b.Loop() {
		structure.Folds(file.Tree, func(o int) int { return int(file.Doc.OffsetToPosition(o, docstore.EncodingUTF16).Line) })
	}
}

func BenchmarkReferencesLocal(b *testing.B) {
	file := benchFile()
	offset := strings.LastIndex(text, "$count")
	for b.Loop() {
		if _, err := navigation.References(b.Context(), file, offset+1, benchEnv(), nil, true); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReferencesGlobalFunction(b *testing.B) {
	file := benchFile()
	offset := strings.LastIndex(text, "\nstep_") + 1
	for b.Loop() {
		if _, err := navigation.References(b.Context(), file, offset, benchEnv(), nil, true); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDefinition(b *testing.B) {
	file := benchFile()
	offset := strings.LastIndex(text, "$count") + 1
	for b.Loop() {
		if _, err := navigation.Definition(b.Context(), file, offset, benchEnv(), nil, navigation.ModeDefinition); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkInlayHints(b *testing.B) {
	file := benchFile()
	lookup := func(name string) (*parser.Tree, parser.Function, bool) {
		fn, ok := file.Tree.FunctionNamed(name)
		return file.Tree, fn, ok
	}
	for b.Loop() {
		assist.InlayHints(file, parser.Span{Start: 0, End: len(text)}, lookup, benchEnv())
	}
}

func BenchmarkCodeActions(b *testing.B) {
	tree := parser.Parse(text, dialect.Bash)
	offset := strings.LastIndex(text, "count=0")
	for b.Loop() {
		actions.Actions(actions.Input{Tree: tree, Selection: parser.Span{Start: offset, End: offset + 5}, LocalSupported: true})
	}
}

func BenchmarkFormat(b *testing.B) {
	tree := parser.Parse(text, dialect.Bash)
	for b.Loop() {
		formatted, err := format.Format(tree, format.Options{})
		if err != nil {
			b.Fatal(err)
		}
		format.Edits(text, formatted)
	}
}

func BenchmarkFormatFormatted(b *testing.B) {
	formatted, err := format.Format(parser.Parse(text, dialect.Bash), format.Options{})
	if err != nil {
		b.Fatal(err)
	}
	tree := parser.Parse(formatted, dialect.Bash)
	for b.Loop() {
		again, err := format.Format(tree, format.Options{})
		if err != nil {
			b.Fatal(err)
		}
		format.Edits(formatted, again)
	}
}

func BenchmarkSelection(b *testing.B) {
	tree := parser.Parse(text, dialect.Bash)
	offset := strings.LastIndex(text, "$count")
	for b.Loop() {
		structure.SelectionPath(tree, offset)
	}
}

func BenchmarkDocumentChange(b *testing.B) {
	store := docstore.NewStore()
	store.Open(lsp.TextDocumentItem{URI: "file:///large.bash", Version: 1, Text: text})
	version := int32(1)
	change := []lsp.TextDocumentContentChangeEvent{{Range: &lsp.Range{Start: lsp.Position{Line: 5000}, End: lsp.Position{Line: 5000}}, Text: "x"}}
	for b.Loop() {
		version++
		if err := store.Change("file:///large.bash", version, change); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDetectDialect(b *testing.B) {
	for b.Loop() {
		dialect.Detect(nil, dialect.Input{URI: "file:///w/large", Text: text})
	}
}

func largeWorkspace(b *testing.B) (*workspace.Workspace, *workspace.File) {
	b.Helper()

	root := b.TempDir()
	for i := range 1500 {
		dir := filepath.Join(root, fmt.Sprintf("pkg%02d", i%30))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatal(err)
		}
		body := fmt.Sprintf("source ../lib.sh\nhelper_%d() { shared \"$1\"; }\nhelper_%d x\n", i, i)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%04d.sh", i)), []byte(body), 0o600); err != nil {
			b.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "lib.sh"), []byte("shared() { echo \"$1\"; }\n"), 0o600); err != nil {
		b.Fatal(err)
	}
	ws := workspace.New(func(string) (*workspace.File, bool) { return nil, false }, func(string) *dialect.Dialect { return nil })
	ws.SetRoots([]string{root})
	lib, err := ws.Load(filepath.Join(root, "lib.sh"))
	if err != nil {
		b.Fatal(err)
	}
	return ws, lib
}

func BenchmarkWorkspaceReferences(b *testing.B) {
	ws, lib := largeWorkspace(b)
	benchmarkReferences(b, ws, lib)
}

func BenchmarkWorkspaceReferencesWatched(b *testing.B) {
	ws, lib := largeWorkspace(b)
	ws.SetWatched(true)
	benchmarkReferences(b, ws, lib)
}

func benchmarkReferences(b *testing.B, ws *workspace.Workspace, lib *workspace.File) {
	b.Helper()

	env := navigation.Env{Loader: ws, Roots: ws.Roots()}
	if _, err := navigation.References(b.Context(), lib, 0, env, ws.ShellFiles, true); err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		refs, err := navigation.References(b.Context(), lib, 0, env, ws.ShellFiles, true)
		if err != nil || len(refs) != 1501 {
			b.Fatalf("references = %d, %v", len(refs), err)
		}
	}
}

func BenchmarkWorkspaceScan(b *testing.B) {
	ws, _ := largeWorkspace(b)
	for b.Loop() {
		if _, err := ws.ShellFiles(b.Context()); err != nil {
			b.Fatal(err)
		}
	}
}
