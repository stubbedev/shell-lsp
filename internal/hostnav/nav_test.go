package hostnav

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

type tree struct {
	t     *testing.T
	dir   string
	ws    *workspace.Workspace
	files map[string]string
}

func newTree(t *testing.T, files map[string]string) *tree {
	t.Helper()
	return &tree{t: t, dir: writeTree(t, files), ws: newWorkspace(), files: files}
}

func (tr *tree) path(name string) string { return filepath.Join(tr.dir, name) }

func (tr *tree) all(ctx context.Context) ([]*workspace.File, error) {
	var out []*workspace.File
	names := make([]string, 0, len(tr.files))
	for name := range tr.files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		f, err := tr.ws.Load(tr.path(name))
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, ctx.Err()
}

func (tr *tree) group(name string) *Group {
	tr.t.Helper()
	return groupFor(tr.t, tr.ws, tr.path(name), tr.all)
}

func (tr *tree) offset(name, sub string, nth int) int {
	tr.t.Helper()
	text := tr.files[name]
	at := -1
	for range nth + 1 {
		i := strings.Index(text[at+1:], sub)
		if i < 0 {
			tr.t.Fatalf("%q occurs fewer than %d times in %s", sub, nth+1, name)
		}
		at += i + 1
	}
	return at + 1
}

func (tr *tree) at(name, sub string, nth int) (*Group, Symbol) {
	tr.t.Helper()
	g := tr.group(name)
	sym, ok := g.At(tr.offset(name, sub, nth))
	if !ok {
		tr.t.Fatalf("no symbol at %q #%d in %s", sub, nth, name)
	}
	return g, sym
}

func (tr *tree) shellAt(name, sub string, nth int) (*Group, Symbol) {
	tr.t.Helper()
	g := tr.group(name)
	host := tr.offset(name, sub, nth)
	offset, inside := g.Self.Doc.Positions(0).Offset(g.Self.Doc.Source().OffsetToPosition(host, 0))
	if !inside {
		tr.t.Fatalf("%q #%d in %s is not in the shell", sub, nth, name)
	}
	sym, ok := g.ShellSymbol(offset)
	if !ok {
		tr.t.Fatalf("no host symbol behind the shell at %q #%d in %s", sub, nth, name)
	}
	return g, sym
}

func (tr *tree) describe(occs []Occurrence) []string {
	out := make([]string, len(occs))
	for i, o := range occs {
		name, _ := filepath.Rel(tr.dir, o.File.Path)
		name = filepath.ToSlash(name)
		text := o.File.Doc.Source().Text()
		line := strings.Count(text[:o.Span.Start], "\n") + 1
		out[i] = fmt.Sprintf("%s:%d:%s", name, line, text[o.Span.Start:o.Span.End])
	}
	return out
}

func (tr *tree) locations(locs []Location) []string {
	occs := make([]Occurrence, len(locs))
	for i, l := range locs {
		occs[i] = Occurrence{File: l.File, Span: l.Span}
	}
	return tr.describe(occs)
}

func expectStrings(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s\n got %q\nwant %q", what, got, want)
	}
}

func TestJustImportsShareOneNamespace(t *testing.T) {
	t.Parallel()

	tr := newTree(t, map[string]string{
		"justfile":        "import 'ci/release.just'\n\nbuild:\n    cargo build\n\ndefault: build release\n",
		"ci/release.just": "release: build\n    echo {{version}}\n\nversion := \"1\"\n",
	})
	g, sym := tr.at("justfile", "release", 1)
	expectStrings(t, "definition of release", tr.locations(g.definitions(sym)), []string{"ci/release.just:1:release"})
	g, sym = tr.at("ci/release.just", "build", 0)
	expectStrings(t, "references of build from the imported file", tr.describe(References(g, sym, true)), []string{
		"ci/release.just:1:build",
		"justfile:3:build",
		"justfile:6:build",
	})
	if findings := tr.group("ci/release.just").Findings(); len(findings) > 0 {
		t.Errorf("the imported file sees the root's recipes, yet reports %+v", findings)
	}
	expectStrings(t, "links", linkTargets(tr, tr.group("justfile")), []string{"ci/release.just"})
}

func linkTargets(tr *tree, g *Group) []string {
	var out []string
	for _, l := range g.Links() {
		rel, _ := filepath.Rel(tr.dir, l.Target.Path)
		out = append(out, filepath.ToSlash(rel))
	}
	return out
}

func TestJustModulesAreQualified(t *testing.T) {
	t.Parallel()

	tr := newTree(t, map[string]string{
		"justfile":       "mod tools\n\nall: tools::lint\n    echo done\n",
		"tools/mod.just": "lint:\n    shellcheck *.sh\n\nhelper: lint\n",
	})
	g, sym := tr.at("justfile", "lint", 0)
	expectStrings(t, "definition of tools::lint", tr.locations(g.definitions(sym)), []string{"tools/mod.just:1:lint"})
	g, sym = tr.at("justfile", "tools", 1)
	expectStrings(t, "definition of the module", tr.locations(g.definitions(sym)), []string{"tools/mod.just:1:"})
	g, sym = tr.at("tools/mod.just", "lint", 0)
	expectStrings(t, "references of lint include the parent's tools::lint", tr.describe(References(g, sym, true)), []string{
		"justfile:3:lint",
		"tools/mod.just:1:lint",
		"tools/mod.just:4:lint",
	})
	if err := g.PrepareRename(Symbol{}); !errors.Is(err, navigation.ErrNotRenamable) {
		t.Errorf("renaming nothing: %v", err)
	}
	g, sym = tr.at("justfile", "tools", 0)
	if err := g.PrepareRename(sym); !errors.Is(err, navigation.ErrNotRenamable) {
		t.Errorf("renaming a module found by its name = %v, want ErrNotRenamable", err)
	}
	items := g.Complete(tr.offset("justfile", "lint", 0) + 2)
	if !slices.ContainsFunc(items, func(it Item) bool { return it.Label == "lint" }) || slices.ContainsFunc(items, func(it Item) bool { return it.Label == "all" }) {
		t.Errorf("completion after tools:: = %+v, want the module's recipes only", items)
	}
}

func TestJustExportedVariablesCrossIntoTheShell(t *testing.T) {
	t.Parallel()

	tr := newTree(t, map[string]string{
		"justfile": "export TOKEN := \"x\"\nlocal := \"y\"\n\nrun $user name:\n    echo $TOKEN $user $name {{local}}\n\nshadow:\n    TOKEN=1; echo $TOKEN\n",
	})
	g, sym := tr.at("justfile", "TOKEN", 0)
	expectStrings(t, "references of TOKEN", tr.describe(References(g, sym, true)), []string{"justfile:1:TOKEN", "justfile:5:TOKEN"})
	g, sym = tr.shellAt("justfile", "TOKEN", 1)
	expectStrings(t, "definition of $TOKEN", tr.locations(g.definitions(sym)), []string{"justfile:1:TOKEN"})
	if !strings.Contains(g.Describe(sym).Markdown, "exported to recipes") {
		t.Errorf("hover of $TOKEN = %q", g.Describe(sym).Markdown)
	}
	g, sym = tr.at("justfile", "user", 0)
	expectStrings(t, "references of the exported parameter", tr.describe(References(g, sym, true)), []string{"justfile:4:user", "justfile:5:user"})
	g, sym = tr.at("justfile", "name", 0)
	expectStrings(t, "a parameter that is not exported stays out of the shell", tr.describe(References(g, sym, true)), []string{"justfile:4:name"})
	if _, err := g.Rename(sym, "full-name"); err != nil {
		t.Errorf("renaming a parameter only just sees to a just name: %v", err)
	}
	g, sym = tr.at("justfile", "TOKEN", 0)
	if _, err := g.Rename(sym, "my-token"); !errors.Is(err, navigation.ErrInvalidName) {
		t.Errorf("renaming TOKEN, which the shell reads, to my-token = %v, want ErrInvalidName", err)
	}
	if _, err := g.Rename(sym, "local"); !errors.Is(err, navigation.ErrNameConflict) {
		t.Errorf("renaming TOKEN to local = %v, want ErrNameConflict", err)
	}
	occs, err := g.Rename(sym, "SECRET")
	if err != nil {
		t.Fatal(err)
	}
	expectStrings(t, "rename edits", tr.describe(occs), []string{"justfile:1:TOKEN", "justfile:5:TOKEN"})
}

func TestJustRecipesCalledFromTheShell(t *testing.T) {
	t.Parallel()

	tr := newTree(t, map[string]string{
		"justfile": "build:\n    cargo build\n\nci:\n    just build\n    just --justfile other.just build\n    {{just_executable()}} build\n",
	})
	g, sym := tr.at("justfile", "build", 0)
	expectStrings(t, "references of build", tr.describe(References(g, sym, true)), []string{"justfile:1:build", "justfile:5:build", "justfile:7:build"})
	g, sym = tr.shellAt("justfile", "build", 2)
	expectStrings(t, "definition of build from just build", tr.locations(g.definitions(sym)), []string{"justfile:1:build"})
	occs, err := g.Rename(sym, "build-all")
	if err != nil {
		t.Fatalf("renaming a recipe the shell calls to a hyphenated name: %v", err)
	}
	expectStrings(t, "rename edits", tr.describe(occs), []string{"justfile:1:build", "justfile:5:build", "justfile:7:build"})
}

func TestJustHoverAndCompletion(t *testing.T) {
	t.Parallel()

	tr := newTree(t, map[string]string{
		"justfile": "set shell := [\"bash\", \"-c\"]\n\n# Compile everything\n[private]\nbuild target=\"debug\":\n    echo {{uppercase(target)}}\n\nversion := \"1\"\n",
	})
	g := tr.group("justfile")
	for _, tt := range []struct{ sub, want string }{
		{"shell", "run recipe lines and backticks"},
		{"private", "Hide the item"},
		{"uppercase", "upper case"},
		{"build", "Compile everything"},
	} {
		h, ok := g.Hover(tr.offset("justfile", tt.sub, 0))
		if !ok || !strings.Contains(h.Markdown, tt.want) {
			t.Errorf("hover on %s = %q, %v, want it to mention %q", tt.sub, h.Markdown, ok, tt.want)
		}
	}
	items := g.Complete(tr.offset("justfile", "target)", 0))
	for _, label := range []string{"target", "version", "uppercase", "HEX"} {
		if !slices.ContainsFunc(items, func(it Item) bool { return it.Label == label }) {
			t.Errorf("completion in the interpolation lacks %s", label)
		}
	}
}

func TestJustUnknownNamesWaitForMissingImports(t *testing.T) {
	t.Parallel()

	tr := newTree(t, map[string]string{"justfile": "import? 'generated.just'\nimport 'gone.just'\n\nall: generated\n"})
	var codes []string
	for _, f := range tr.group("justfile").Findings() {
		codes = append(codes, f.Code.String())
	}
	expectStrings(t, "findings", codes, []string{"SL5018"})
}

func TestMakeIncludesShareVariablesAndTargets(t *testing.T) {
	t.Parallel()

	tr := newTree(t, map[string]string{
		"Makefile":    "DIR := mk\ninclude $(DIR)/flags.mk\ninclude rules.inc\n\nall: app\n\t$(CC) $(CFLAGS) -o app main.c\n\t$(MAKE) clean\n",
		"mk/flags.mk": "CFLAGS := -O2\n",
		"rules.inc":   "clean:\n\trm -f app\n",
	})
	g, sym := tr.at("mk/flags.mk", "CFLAGS", 0)
	expectStrings(t, "references of CFLAGS from the included file", tr.describe(References(g, sym, true)), []string{"Makefile:6:CFLAGS", "mk/flags.mk:1:CFLAGS"})
	g, sym = tr.shellAt("Makefile", "clean", 0)
	expectStrings(t, "definition of $(MAKE) clean", tr.locations(g.definitions(sym)), []string{"rules.inc:1:clean"})
	expectStrings(t, "links", linkTargets(tr, tr.group("Makefile")), []string{"mk/flags.mk", "rules.inc"})
	if findings := tr.group("Makefile").Findings(); len(findings) > 0 {
		t.Errorf("findings = %+v", findings)
	}
}

func TestMakeExportedVariablesAndTargetScopes(t *testing.T) {
	t.Parallel()

	tr := newTree(t, map[string]string{
		"Makefile": "TOKEN := x\nexport TOKEN\nMODE := slow\n\nfast: MODE = fast\nfast:\n\t@echo $(MODE) $$TOKEN\n\nslow:\n\t@echo $(MODE)\n",
	})
	g, sym := tr.at("Makefile", "TOKEN", 0)
	expectStrings(t, "references of TOKEN", tr.describe(References(g, sym, true)), []string{"Makefile:1:TOKEN", "Makefile:2:TOKEN", "Makefile:7:TOKEN"})
	g, sym = tr.at("Makefile", "MODE", 2)
	expectStrings(t, "definition of MODE in fast's recipe", tr.locations(g.definitions(sym)), []string{"Makefile:5:MODE"})
	g, sym = tr.at("Makefile", "MODE", 3)
	expectStrings(t, "definition of MODE in slow's recipe", tr.locations(g.definitions(sym)), []string{"Makefile:3:MODE"})
	if _, ok := g.At(tr.offset("Makefile", "fast", 0)); !ok {
		t.Error("no symbol on the target-specific assignment's target")
	}
	h, ok := g.Hover(tr.offset("Makefile", "TOKEN", 0))
	if !ok || h.Markdown == "" || !strings.Contains(h.Markdown, "exported") {
		t.Errorf("hover on TOKEN = %q", h.Markdown)
	}
	if items := g.Complete(tr.offset("Makefile", "MODE)", 1)); !slices.ContainsFunc(items, func(it Item) bool { return it.Label == "TOKEN" && it.Kind == model.KindVariable }) {
		t.Errorf("completion in $(...) = %+v", items)
	}
}

type twoCopies struct {
	*workspace.Workspace
}

func (l twoCopies) LoadAs(path string, language host.Language) (*workspace.File, error) {
	f, err := l.Workspace.LoadAs(path, language)
	if err != nil {
		return nil, err
	}
	return workspace.Parse(f.Path, f.Doc.Source(), nil, func(dialect.Input) dialect.Detection { return f.Detection }), nil
}

func TestGroupsSurviveSeveralCopiesOfOneFile(t *testing.T) {
	t.Parallel()

	tr := newTree(t, map[string]string{
		"justfile": "import 'ci.just'\nexport TOKEN := 'x'\nbuild:\n    echo $TOKEN\n",
		"ci.just":  "release: build\n    echo $TOKEN\n",
	})
	self, err := tr.ws.Load(tr.path("ci.just"))
	if err != nil {
		t.Fatal(err)
	}
	g, err := Build(context.Background(), self, Env{Loader: twoCopies{tr.ws}, Files: tr.all})
	if err != nil {
		t.Fatal(err)
	}
	if findings := g.Findings(); len(findings) > 0 {
		t.Errorf("findings = %+v", findings)
	}
	sym, ok := g.At(tr.offset("ci.just", "build", 0))
	if !ok || len(References(g, sym, true)) != 2 {
		t.Errorf("references of build across copies = %+v", References(g, sym, true))
	}
}
