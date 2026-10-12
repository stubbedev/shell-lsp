package navigation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/workspace"
)

type fixture struct {
	root string
	ws   *workspace.Workspace
	env  Env
}

func newFixture(t *testing.T, files map[string]string) fixture {
	t.Helper()

	root := t.TempDir()
	for name, text := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ws := workspace.New(func(string) (*workspace.File, bool) { return nil, false }, workspace.DetectShell)
	ws.SetRoots([]string{root})
	return fixture{
		root: root,
		ws:   ws,
		env:  Env{Loader: ws, Roots: []string{root}, Home: filepath.Join(root, "home"), PathDirs: []string{filepath.Join(root, "bin")}},
	}
}

func (f fixture) files(ctx context.Context) ([]*workspace.File, error) {
	paths, err := f.ws.ShellFiles(ctx)
	if err != nil {
		return nil, err
	}
	return f.ws.LoadAll(ctx, paths, 1, func(int, int) {})
}

func (f fixture) load(t *testing.T, name string) *workspace.File {
	t.Helper()

	file, err := f.ws.Load(filepath.Join(f.root, name))
	if err != nil {
		t.Fatalf("Load(%s) error = %v", name, err)
	}
	return file
}

func (f fixture) define(t *testing.T, name, needle string, nth int, mode Mode) []Location {
	t.Helper()

	file := f.load(t, name)
	text := file.Tree.Text()
	offset := -1
	for range nth + 1 {
		i := strings.Index(text[offset+1:], needle)
		if i < 0 {
			t.Fatalf("needle %q #%d not in %s", needle, nth, name)
		}
		offset += i + 1
	}
	locs, err := Definition(t.Context(), file, offset, f.env, f.files, mode)
	if err != nil {
		t.Fatalf("Definition error = %v", err)
	}
	return locs
}

func describe(f fixture, locs []Location) []string {
	out := make([]string, len(locs))
	for i, l := range locs {
		rel, _ := filepath.Rel(f.root, l.File.Path)
		out[i] = filepath.ToSlash(rel) + ":" + l.File.Tree.Text()[l.Span.Start:l.Span.End]
	}
	return out
}

func TestSourceIdioms(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"main.sh": `#!/bin/bash
here=$(dirname "$0")
lib_dir="$here/lib"
source ./lib/a.sh
. "${BASH_SOURCE%/*}/lib/b.sh"
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/c.sh"
source "$lib_dir/d.sh"
# shellcheck source=SCRIPTDIR/lib/e.sh
source "$UNKNOWN/e.sh"
source ~/rc.sh
source tool.sh
source "$missing_dir/never.sh"
# shellcheck source=/dev/null
source ./lib/a.sh
`,
		"lib/a.sh":     "a() { :; }\n",
		"lib/b.sh":     "b() { :; }\n",
		"lib/c.sh":     "c() { :; }\n",
		"lib/d.sh":     "d() { :; }\n",
		"lib/e.sh":     "e() { :; }\n",
		"home/rc.sh":   "rc() { :; }\n",
		"bin/tool.sh":  "tool() { :; }\n",
		"unrelated.sh": "x() { :; }\n",
	})
	refs := Sources(f.load(t, "main.sh"), f.env)
	want := []string{"lib/a.sh", "lib/b.sh", "lib/c.sh", "lib/d.sh", "lib/e.sh", "home/rc.sh", "bin/tool.sh", "", ""}
	if len(refs) != len(want) {
		t.Fatalf("Sources() = %d refs, want %d", len(refs), len(want))
	}
	for i, ref := range refs {
		got := ""
		if ref.Target != nil {
			rel, _ := filepath.Rel(f.root, ref.Target.Path)
			got = filepath.ToSlash(rel)
		}
		if got != want[i] {
			t.Errorf("source #%d resolved to %q, want %q", i, got, want[i])
		}
	}
}

func TestSourceDirectiveValuesNavigate(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"main.sh":     "#!/bin/bash\n  # shellcheck source=lib/util.sh\nsource \"$LIB/util.sh\"\nsource ./lib/util.sh\n",
		"lib/util.sh": "util() { :; }\n",
	})
	main := f.load(t, "main.sh")
	refs := Sources(main, f.env)
	if len(refs) != 2 || refs[0].Directive == nil || refs[1].Directive != nil {
		t.Fatalf("Sources() = %+v", refs)
	}
	if got := main.Tree.Text()[refs[0].Directive.Start:refs[0].Directive.End]; got != "lib/util.sh" {
		t.Errorf("directive span covers %q", got)
	}
	locs := f.define(t, "main.sh", "lib/util.sh", 0, ModeDefinition)
	if len(locs) != 1 || filepath.Base(locs[0].File.Path) != "util.sh" {
		t.Errorf("definition on the source= value = %v", describe(f, locs))
	}
}

func TestChainIsTransitiveAndCycleSafe(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"main.sh": "source ./one.sh\n",
		"one.sh":  "source ./two.sh\n",
		"two.sh":  "source ./one.sh\nsource ./main.sh\n",
	})
	chain := Chain(f.load(t, "main.sh"), f.env)
	if len(chain) != 2 || filepath.Base(chain[0].Path) != "one.sh" || filepath.Base(chain[1].Path) != "two.sh" {
		t.Errorf("Chain() = %v", chain)
	}
	if _, complete := Closure(f.load(t, "main.sh"), f.env); !complete {
		t.Error("Closure() is incomplete although every source resolves")
	}
}

func TestClosureIsIncompleteWhenASourceDoesNotResolve(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"main.sh": "source ./one.sh\n",
		"one.sh":  "source \"$PLUGIN\"\n",
	})
	files, complete := Closure(f.load(t, "main.sh"), f.env)
	if complete || len(files) != 1 || filepath.Base(files[0].Path) != "one.sh" {
		t.Errorf("Closure() = %v, %t; want one.sh and incomplete", files, complete)
	}
}

func TestDefinitions(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"main.sh": `#!/bin/bash
source ./lib.sh
declare -a items=(x)
count=0
greet() {
	local count=1
	echo "$count"
}
greet
helper
echo "$count" "$LIB_VAR" "${items[@]}"
shared
`,
		"lib.sh":   "LIB_VAR=1\nhelper() { :; }\nshared() { :; }\n",
		"other.sh": "shared() { echo override; }\nunsourced() { :; }\n",
		"more.sh":  "unsourced() { :; }\n",
	})
	tests := []struct {
		needle string
		nth    int
		mode   Mode
		want   []string
	}{
		{needle: "greet\nhelper", mode: ModeDefinition, want: []string{"main.sh:greet"}},
		{needle: "helper", mode: ModeDefinition, want: []string{"lib.sh:helper"}},
		{needle: "$count", nth: 0, mode: ModeDefinition, want: []string{"main.sh:count"}},
		{needle: "LIB_VAR", mode: ModeDefinition, want: []string{"lib.sh:LIB_VAR"}},
		{needle: "./lib.sh", mode: ModeDefinition, want: []string{"lib.sh:"}},
		{needle: "items[@]", mode: ModeTypeDefinition, want: []string{"main.sh:items"}},
		{needle: "$count\" \"$LIB", mode: ModeTypeDefinition, want: []string{}},
		{needle: "shared\n", nth: 0, mode: ModeDefinition, want: []string{"lib.sh:shared"}},
		{needle: "shared\n", nth: 0, mode: ModeImplementation, want: []string{"lib.sh:shared", "other.sh:shared"}},
	}
	for _, tc := range tests {
		got := describe(f, f.define(t, "main.sh", tc.needle, tc.nth, tc.mode))
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("%q mode %d = %v, want %v", tc.needle, tc.mode, got, tc.want)
		}
	}

	local := describe(f, f.define(t, "main.sh", "$count", 0, ModeDefinition))
	if strings.Join(local, ",") != "main.sh:count" {
		t.Errorf("local count = %v", local)
	}
	got := f.define(t, "main.sh", "$count", 0, ModeDefinition)
	if text := got[0].File.Tree.Text(); !strings.HasPrefix(text[got[0].Span.Start-6:], "local count") {
		t.Errorf("reference inside greet resolved to %q, want the local", text[got[0].Span.Start-6:got[0].Span.End])
	}
	outer := f.define(t, "main.sh", "$count", 1, ModeDefinition)
	if text := outer[0].File.Tree.Text(); text[outer[0].Span.Start:outer[0].Span.Start+7] != "count=0" {
		t.Errorf("reference outside greet resolved to %q, want the global", text[outer[0].Span.Start:outer[0].Span.End])
	}
}

func TestWorkspaceFallbackForUnsourcedFunctions(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"main.sh":   "unsourced\n",
		"other.sh":  "unsourced() { :; }\n",
		"script":    "#!/usr/bin/env bash\nunsourced() { :; }\n",
		"notes.txt": "unsourced() { :; }\n",
	})
	if err := os.Chmod(filepath.Join(f.root, "script"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := describe(f, f.define(t, "main.sh", "unsourced", 0, ModeDefinition))
	if strings.Join(got, ",") != "other.sh:unsourced,script:unsourced" {
		t.Errorf("workspace fallback = %v", got)
	}
}

func TestWorkspaceFilesError(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{"main.sh": "nowhere\n"})
	file := f.load(t, "main.sh")
	_, err := Definition(t.Context(), file, 0, f.env, func(context.Context) ([]*workspace.File, error) { return nil, os.ErrPermission }, ModeDefinition)
	if err == nil {
		t.Error("workspace listing error swallowed")
	}
}
