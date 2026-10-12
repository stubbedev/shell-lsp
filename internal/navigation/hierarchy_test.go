package navigation

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func callNames(calls []Call) []string {
	out := make([]string, len(calls))
	for i, c := range calls {
		out[i] = c.Item.Name + "x" + string(rune('0'+len(c.Spans)))
	}
	return out
}

func TestRelativeNamesUseSlashes(t *testing.T) {
	t.Parallel()

	root := filepath.FromSlash("/proj")
	script := filepath.Join(root, "bin", "run.sh")
	tests := map[string]string{
		filepath.Join(root, "bin", "lib", "util.sh"): "lib/util.sh",
		filepath.Join(root, "bin", "..x.sh"):         "..x.sh",
		filepath.Join(root, "shared", "util.sh"):     filepath.ToSlash(filepath.Join(root, "shared", "util.sh")),
	}
	for target, want := range tests {
		if got := RelativeName(script, target); got != want {
			t.Errorf("RelativeName(%q, %q) = %q, want %q", script, target, got, want)
		}
	}
}

func TestCallHierarchy(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"main.sh": "source ./lib.sh\nbuild() {\n\tlog start\n\tcompile\n\tlog done\n}\ncompile() {\n\tlog compiling\n}\nbuild\nlog top\n",
		"lib.sh":  "log() { echo \"$*\"; }\n",
	})
	main := f.load(t, "main.sh")
	items := PrepareCallHierarchy(main, f.offset(t, "main.sh", "build() {", 0), f.env)
	if len(items) != 1 || items[0].Name != "build" {
		t.Fatalf("prepare = %+v", items)
	}
	if got := strings.Join(callNames(OutgoingCalls(items[0], f.env)), ","); got != "logx2,compilex1" {
		t.Errorf("outgoing = %s", got)
	}
	logItems := PrepareCallHierarchy(main, f.offset(t, "main.sh", "log start", 0), f.env)
	if len(logItems) != 1 || logItems[0].File.Path != f.load(t, "lib.sh").Path {
		t.Fatalf("prepare on a call of a sourced function = %+v", logItems)
	}
	incoming, err := IncomingCalls(t.Context(), logItems[0], f.env, f.files)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(callNames(incoming), ","); got != "buildx2,compilex1,main.shx1" {
		t.Errorf("incoming = %s", got)
	}
	top := fileItem(main)
	if got := strings.Join(callNames(OutgoingCalls(top, f.env)), ","); got != "buildx1,logx1" {
		t.Errorf("top-level outgoing = %s", got)
	}
}

func TestMonikers(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"main.sh":    "source ./sub/lib.sh\nrun() { local tmp=1; }\nhelper\nexport VERSION=1\n",
		"sub/lib.sh": "helper() { :; }\n",
	})
	main := f.load(t, "main.sh")
	tests := []struct {
		needle string
		want   Moniker
	}{
		{needle: "./sub", want: Moniker{Identifier: "sub/lib.sh", Kind: MonikerImport, Workspace: true}},
		{needle: "run", want: Moniker{Identifier: "main.sh#run", Kind: MonikerExport, Workspace: true}},
		{needle: "helper\n", want: Moniker{Identifier: "sub/lib.sh#helper", Kind: MonikerImport, Workspace: true}},
		{needle: "VERSION", want: Moniker{Identifier: "$VERSION", Kind: MonikerExport, Workspace: true}},
		{needle: "tmp", want: Moniker{Identifier: "main.sh#tmp/1", Kind: MonikerLocal}},
	}
	for _, tc := range tests {
		got, ok := MonikerAt(main, f.offset(t, "main.sh", tc.needle, 0), f.env)
		if !ok || got != tc.want {
			t.Errorf("moniker %q = %+v, %t; want %+v", tc.needle, got, ok, tc.want)
		}
	}
}

func TestInlineValues(t *testing.T) {
	t.Parallel()

	text := "top=1\nf() {\n\tlocal a=1\n\tb=$a\n\techo \"$b $top\"\n\tlater=2\n}\nother=$top\n"
	tree := parser.Parse(text, dialect.Bash)
	stopped := strings.Index(text, "echo")
	var got []string
	for _, v := range InlineValues(tree, parser.Span{Start: 0, End: len(text)}, stopped) {
		got = append(got, v.Name)
	}
	if strings.Join(got, ",") != "a,b,a" {
		t.Errorf("inline values = %v", got)
	}
	var top []string
	for _, v := range InlineValues(tree, parser.Span{Start: 0, End: len(text)}, len(text)-1) {
		top = append(top, v.Name)
	}
	if strings.Join(top, ",") != "top,other,top" {
		t.Errorf("top-level inline values = %v", top)
	}
}
