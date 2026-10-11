package lint

import (
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func nested(open, inner, close string, depth int) string {
	return strings.Repeat(open, depth) + inner + strings.Repeat(close, depth)
}

func allocatedBytes(run func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	run()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

func TestCheckAllocatesLinearlyInNestingDepth(t *testing.T) {
	cases := []struct {
		name     string
		dialect  dialect.Dialect
		optional []string
		script   func(depth int) string
	}{
		{"nested brace expansions", dialect.Posix, nil, func(depth int) string { return "echo " + nested("{a,", "b", "}", depth) + "\n" }},
		{"masked returns in process substitutions", dialect.Bash, []string{optionalMaskedReturns}, func(depth int) string { return "cat " + nested("<(", "x", ")", depth) + "\n" }},
		{"nested subscripts", dialect.Bash, nil, func(depth int) string { return "echo " + nested("${a[", "1", "]}", depth) + "\n" }},
	}
	const shallow, deep = 100, 300
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lint := func(depth int) uint64 {
				tree := parser.Parse(tc.script(depth), tc.dialect)
				return allocatedBytes(func() { Check(tree, nil, Options{Optional: tc.optional}) })
			}
			small, large := lint(shallow), lint(deep)
			if large > 5*small {
				t.Errorf("linting depth %d allocated %d bytes, depth %d %d bytes: more than linear growth", shallow, small, deep, large)
			}
		})
	}
}

func casePatterns(n int, pattern func(i int) string) string {
	var sb strings.Builder
	sb.WriteString("case $x in\n")
	for i := range n {
		sb.WriteString(pattern(i) + ") ;;\n")
	}
	sb.WriteString("esac\n")
	return sb.String()
}

func TestCheckFinishesLargeInputsQuickly(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		script string
	}{
		{"unclosed parameter expansions", "echo " + strings.Repeat("${", 50_000) + "\n"},
		{"one command with many arguments", "echo " + strings.Repeat("a ", 200_000) + "\n"},
		{"many literal case patterns", casePatterns(100_000, func(i int) string { return "p" + strconv.Itoa(i) })},
		{"many glob case patterns", casePatterns(20_000, func(i int) string { return "p" + strconv.Itoa(i) + "*" })},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tree := parser.Parse(tc.script, dialect.Bash)
			start := time.Now()
			Check(tree, nil, Options{})
			if elapsed := time.Since(start); elapsed > 3*time.Second {
				t.Errorf("linting took %v", elapsed)
			}
		})
	}
}
