package perf

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/completion"
	"github.com/stubbedev/shell-lsp/internal/diagnostics"
	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/hover"
	"github.com/stubbedev/shell-lsp/internal/manpage"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/semantic"
	"github.com/stubbedev/shell-lsp/internal/structure"
)

func largeScript(lines int) string {
	var sb strings.Builder
	sb.WriteString("#!/usr/bin/env bash\nset -euo pipefail\n")
	for i := 0; strings.Count(sb.String(), "\n") < lines; i++ {
		fmt.Fprintf(&sb, `# Handles step %[1]d.
step_%[1]d() {
	local name="$1" count=0
	local -a items=(alpha beta "gamma $name")
	for item in "${items[@]}"; do
		case "$item" in
			alpha) (( count += %[1]d )) ;;
			beta|gamma*) echo "${item^^}" >&2 ;;
			*) printf '%%s\n' "$item" ;;
		esac
	done
	if [[ -n "${GLOBAL_%[1]d:-}" && $count -gt 3 ]]; then
		cat <<EOF
step %[1]d: $name $count
EOF
	fi
	result_%[1]d=$(grep -c "$name" /dev/null || true)
}
GLOBAL_%[1]d=value
step_%[1]d "arg%[1]d"
`, i)
	}
	return sb.String()
}

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

var text = largeScript(10000)

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
		semantic.Tokens(tree)
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
