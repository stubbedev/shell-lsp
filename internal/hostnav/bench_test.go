package hostnav

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func largeMakefile(n int) string {
	var sb strings.Builder
	for i := range n {
		fmt.Fprintf(&sb, "V%d := $(V%d) x\n", i, max(i-1, 0))
	}
	sb.WriteString("all:")
	for i := range n {
		fmt.Fprintf(&sb, " t%d", i)
	}
	sb.WriteString("\n")
	for i := range n {
		fmt.Fprintf(&sb, "t%d:\n\t@echo $(V%d) $@\n", i, i)
	}
	return sb.String()
}

func BenchmarkFindingsOfALargeMakefile(b *testing.B) {
	dir := writeTree(b, map[string]string{"Makefile": largeMakefile(3000)})
	ws := newWorkspace()
	file, err := ws.Load(filepath.Join(dir, "Makefile"))
	if err != nil {
		b.Fatal(err)
	}
	h, _ := Of(file)
	b.ResetTimer()
	for b.Loop() {
		g, err := Build(b.Context(), h, Env{Loader: ws})
		if err != nil {
			b.Fatal(err)
		}
		g.Findings()
	}
}
