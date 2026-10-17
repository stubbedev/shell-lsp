package hostnav

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func (tr *tree) sites(list []CallSite) []string {
	out := make([]string, len(list))
	for i, s := range list {
		name, _ := filepath.Rel(tr.dir, s.Peer.File.Path)
		text := s.File.Doc.Source().Text()
		var at []string
		for _, span := range s.Spans {
			at = append(at, fmt.Sprintf("%d:%s", strings.Count(text[:span.Start], "\n")+1, text[span.Start:span.End]))
		}
		out[i] = filepath.ToSlash(name) + " " + s.Name + " from " + strings.Join(at, ",")
	}
	return out
}

func TestJustfileCallHierarchy(t *testing.T) {
	t.Parallel()

	tr := newTree(t, map[string]string{
		"justfile": "import 'ci.just'\n\nbuild: compile\n    just test\n\ncompile:\n    cc main.c\n\ntest: compile\n    ./run\n",
		"ci.just":  "release: build test\n    echo released\n",
	})
	g, build := tr.at("justfile", "build", 0)
	if _, ok := g.CallTarget(tr.offset("justfile", "build", 0)); !ok {
		t.Fatal("build is not a call target")
	}
	if got, want := tr.sites(g.Outgoing(build)), []string{"justfile compile from 3:compile", "justfile test from 4:test"}; !slices.Equal(got, want) {
		t.Errorf("outgoing calls of build = %q, want %q", got, want)
	}
	if got, want := tr.sites(g.Incoming(build)), []string{"ci.just release from 1:build"}; !slices.Equal(got, want) {
		t.Errorf("incoming calls of build = %q, want %q", got, want)
	}
	g, compile := tr.at("justfile", "compile", 1)
	if got, want := tr.sites(g.Incoming(compile)), []string{"justfile build from 3:compile", "justfile test from 9:compile"}; !slices.Equal(got, want) {
		t.Errorf("incoming calls of compile = %q, want %q", got, want)
	}
	again, ok := g.SymbolAt(tr.path("justfile"), build.Span.Start)
	if !ok || again.Name != "build" {
		t.Errorf("SymbolAt(build) = %+v, %v", again, ok)
	}
}

func TestMakefileCallHierarchy(t *testing.T) {
	t.Parallel()

	tr := newTree(t, map[string]string{
		"Makefile": "all: build\n\nbuild: deps\n\t$(MAKE) lint\n\ndeps:\n\ttrue\n\nlint:\n\ttrue\n",
	})
	g, build := tr.at("Makefile", "build", 1)
	if got, want := tr.sites(g.Outgoing(build)), []string{"Makefile deps from 3:deps", "Makefile lint from 4:lint"}; !slices.Equal(got, want) {
		t.Errorf("outgoing calls of build = %q, want %q", got, want)
	}
	if got, want := tr.sites(g.Incoming(build)), []string{"Makefile all from 1:build"}; !slices.Equal(got, want) {
		t.Errorf("incoming calls of build = %q, want %q", got, want)
	}
}
