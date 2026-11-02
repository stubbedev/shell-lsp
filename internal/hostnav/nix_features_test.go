package hostnav

import (
	"context"
	"os"
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/assist"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func TestNixParameterHints(t *testing.T) {
	t.Parallel()

	tr := nixTree(t, "let\n  add = a: b: a + b;\n  b = 1;\n  s = { f = q: q; };\nin\n  [ (add 1 b) (s.f 2) (add { } 3) ]\n")
	want := []string{`"1 b" a:`, `"2) " q:`, `"{ }" a:`, `"3) " b:`}
	if got := tr.hints(nixFile); !slices.Equal(got, want) {
		t.Errorf("hints =\n%q\nwant\n%q", got, want)
	}
}

func TestNixCallHierarchy(t *testing.T) {
	t.Parallel()

	tr := nixTree(t, "let\n  g = y: y;\n  f = x: g x;\n  v = g 1;\nin\n  f 1\n")
	group := tr.group(nixFile)
	callee, ok := group.CallTarget(tr.offset(nixFile, "g", 0))
	if !ok {
		t.Fatal("g is not a call target")
	}
	caller, ok := group.CallTarget(tr.offset(nixFile, "f", 0))
	if !ok {
		t.Fatal("f is not a call target")
	}
	outgoing := group.Outgoing(caller)
	if len(outgoing) != 1 || outgoing[0].Name != "g" {
		t.Errorf("outgoing calls of f = %+v", outgoing)
	}
	if incoming := group.Incoming(callee); len(incoming) != 1 || incoming[0].Name != "f" {
		t.Errorf("incoming calls of g = %+v", incoming)
	}
}

func nixLinkGroup(t *testing.T, tr *tree) *Group {
	t.Helper()
	file, err := tr.ws.Load(tr.path(nixFile))
	if err != nil {
		t.Fatal(err)
	}
	g, err := Build(context.Background(), hostOf(t, file), Env{Loader: tr.ws, Exists: func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && info.Mode().IsRegular()
	}})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestNixLinksAndImportHints(t *testing.T) {
	t.Parallel()

	src := "# see https://example.com/docs.\n[ (import ./sub) (import ./a.nix) ./sub ./missing.nix \"https://nixos.org/${v}\" \"http://a.example/b\" ]\n"
	tr := newTree(t, map[string]string{nixFile: src, "sub/default.nix": "1\n", "a.nix": "2\n"})
	g := nixLinkGroup(t, tr)

	var got []assist.Link
	for _, l := range g.SourceLinks() {
		got = append(got, assist.Link{Span: parser.Span{}, Target: l.Target})
	}
	want := []assist.Link{
		{Target: fileuri.FromPath(tr.path("sub/default.nix"))},
		{Target: fileuri.FromPath(tr.path("a.nix"))},
		{Target: "https://example.com/docs"},
		{Target: "http://a.example/b"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("links =\n%+v\nwant\n%+v", got, want)
	}

	var labels []string
	for _, h := range g.nixHints(parser.Span{End: len(src)}) {
		labels = append(labels, h.Label)
	}
	if want := []string{"→ sub/default.nix"}; !slices.Equal(labels, want) {
		t.Errorf("import hints = %q, want %q", labels, want)
	}
}
