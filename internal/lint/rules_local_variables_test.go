package lint

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func TestSourcedFilesShareFunctionVariables(t *testing.T) {
	t.Parallel()

	src := "source ./lib.sh\nrun_test() {\n  RET=0\n  scratch=1\n  log_test \"$scratch\"\n}\nrun_test\n"
	findings := func(imports []parser.Import) []string {
		var out []string
		for _, f := range Check(parser.Parse(src, dialect.Bash), imports, Options{Optional: []string{"require-local-variables"}}) {
			if f.Code == codeImplicitGlobal {
				out = append(out, src[f.Span.Start:f.Span.End])
			}
		}
		return out
	}
	if got, want := findings(nil), []string{"RET", "scratch"}; !slices.Equal(got, want) {
		t.Fatalf("without the sourced file = %v, want %v", got, want)
	}
	lib := parser.Parse("log_test() { [ \"$RET\" -eq 0 ] && echo \"ok $1\"; }\n", dialect.Bash)
	if got, want := findings([]parser.Import{{Name: "./lib.sh", Tree: lib}}), []string{"scratch"}; !slices.Equal(got, want) {
		t.Errorf("with a sourced file that reads RET = %v, want %v", got, want)
	}
}
