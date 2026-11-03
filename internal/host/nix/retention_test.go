package nix

import (
	"runtime"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

func heapInUse() uint64 {
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.HeapAlloc
}

func TestReleaseReturnsTheRoomEvaluationGrew(t *testing.T) {
	const entries = 200000
	const minFreed = 2 << 20
	p := Link(t.Context(), Request{Self: Analyze("{ a = 1; }\n").Index}, localSource{})
	keys := make([]syntax.Expr, entries)
	for i := range keys {
		keys[i] = &syntax.Ident{}
	}
	for _, key := range keys {
		p.visiting[key] = true
	}
	for _, key := range keys {
		delete(p.visiting, key)
	}
	before := heapInUse()
	p.Release()
	if freed := int64(before) - int64(heapInUse()); freed < minFreed {
		t.Errorf("releasing freed %d bytes of an emptied table of %d entries, want at least %d", freed, entries, minFreed)
	}
	runtime.KeepAlive(keys)
	runtime.KeepAlive(p)
}

func TestCompactedTablesKeepOneCopyOfTheirOwners(t *testing.T) {
	t.Parallel()

	a := AnalyzeDependency("{ a = 1; b = { c = 2; }; }\n")
	r, isRenames := a.Index.Renames.(renames)
	if !isRenames {
		t.Fatalf("Renames is %T", a.Index.Renames)
	}
	if len(r.owners) == 0 || &r.owners[0] != &r.unit.owners[0] {
		t.Error("the published renames hold another copy of the owners than the compacted table")
	}
}

func TestInProvenStringAsksOnlyAboutTheProvenStrings(t *testing.T) {
	t.Parallel()

	src := `{ a = "first"; b = "second ${x} tail"; c = 1; }` + "\n"
	doc := Analyze(src).Document()
	proven := []ProvenString{{Start: strings.Index(src, `"second`)}}
	for _, c := range []struct {
		needle string
		want   bool
	}{
		{"first", false},
		{"second", true},
		{"tail", true},
		{"c = 1", false},
	} {
		if got := doc.InProvenString(strings.Index(src, c.needle), proven); got != c.want {
			t.Errorf("InProvenString at %q = %v, want %v", c.needle, got, c.want)
		}
	}
	if doc.InProvenString(strings.Index(src, "second"), nil) {
		t.Error("a string counts as proven with no proof")
	}
}
