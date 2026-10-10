package lint

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func variableFindings(src string, imports []parser.Import) []string {
	var out []string
	for _, f := range Check(parser.Parse(src, dialect.Bash), imports, Options{}) {
		if f.Code == 2034 || f.Code == 2154 {
			out = append(out, f.Code.String()+" "+src[f.Span.Start:f.Span.End])
		}
	}
	return out
}

func TestSourcedFilesShareVariables(t *testing.T) {
	t.Parallel()

	src := "source ./lib.sh\nlog_level=debug\necho \"$app_name\"\n"
	if got, want := variableFindings(src, nil), []string{"SC2034 log_level=debug", "SC2154 $app_name"}; !slices.Equal(got, want) {
		t.Fatalf("without imports = %v, want %v", got, want)
	}
	lib := parser.Parse("app_name=demo\nlog() { echo \"$log_level\"; }\n", dialect.Bash)
	if got := variableFindings(src, []parser.Import{{Name: "./lib.sh", Tree: lib}}); len(got) != 0 {
		t.Errorf("with the sourced file = %v, want none", got)
	}
}

func TestNameDistance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, candidate string
		distance        int
		close           bool
	}{
		{"LOGLEVEL", "LOG_LEVEL", 1, true},
		{"RETRY_COUNT", "Retry_Count", 1, true},
		{"DATABSE_URI", "DATABASE_URL", 2, true},
		{"ABC", "ABD", 0, false},
		{"HOST", "PORT", 0, false},
	}
	for _, tc := range tests {
		distance, ok := nameDistance(tc.name, tc.candidate)
		if distance != tc.distance || ok != tc.close {
			t.Errorf("nameDistance(%q, %q) = %d, %t; want %d, %t", tc.name, tc.candidate, distance, ok, tc.distance, tc.close)
		}
	}
}
