package lint

import (
	"math"
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
		{"masked returns in process substitutions", dialect.Bash, []string{optionalMaskedReturns.String()}, func(depth int) string { return "cat " + nested("<(", "x", ")", depth) + "\n" }},
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

func fastestChecks(scripts ...string) []time.Duration {
	trees := make([]*parser.Tree, len(scripts))
	fastest := make([]time.Duration, len(scripts))
	for i, script := range scripts {
		trees[i] = parser.Parse(script, dialect.Bash)
		fastest[i] = time.Duration(math.MaxInt64)
	}
	for range 7 {
		for i, tree := range trees {
			runtime.GC()
			start := time.Now()
			Check(tree, nil, Options{})
			fastest[i] = min(fastest[i], time.Since(start))
		}
	}
	return fastest
}

func TestCheckTimeGrowsLinearlyWithInputSize(t *testing.T) {
	cases := []struct {
		name   string
		base   int
		script func(n int) string
	}{
		{"unclosed parameter expansions", 50_000, func(n int) string { return "echo " + strings.Repeat("${", n) + "\n" }},
		{"one command with many arguments", 50_000, func(n int) string { return "echo " + strings.Repeat("a ", n) + "\n" }},
		{"many literal case patterns", 25_000, func(n int) string {
			return casePatterns(n, func(i int) string { return "p" + strconv.Itoa(i) })
		}},
		{"many glob case patterns", 5_000, func(n int) string {
			return casePatterns(n, func(i int) string { return "p" + strconv.Itoa(i) + "*" })
		}},
	}
	const growth, allowed = 4, 8
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			times := fastestChecks(tc.script(tc.base), tc.script(tc.base*growth))
			small, large := times[0], times[1]
			if large > allowed*small {
				t.Errorf("linting %d items took %v, %d items %v: more than %dx for %dx the input", tc.base, small, tc.base*growth, large, allowed, growth)
			}
		})
	}
}
