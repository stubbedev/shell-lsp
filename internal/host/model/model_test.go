package model

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"unsafe"

	"github.com/stubbedev/shell-lsp/internal/lint"
)

var testChecks = Checks{
	Of: func(k Kind) Rules {
		switch k {
		case KindRecipe:
			return Rules{Unknown: lint.CodeJustUnknownRecipe, Arity: lint.CodeJustDependencyArguments, Circular: lint.CodeJustCircular}
		case KindVariable:
			return Rules{Unknown: lint.CodeJustUnknownVariable, Unused: lint.CodeJustUnusedVariable}
		case KindSetting:
			return Rules{Unknown: lint.CodeJustUnknownSetting, UnknownWhen: Regardless}
		default:
			return Rules{}
		}
	},
	Missing: lint.CodeJustMissingFile,
}

func span(start, end int) Span { return Span{Start: start, End: end} }

func codes(findings []lint.Finding) []string {
	out := make([]string, len(findings))
	for i, f := range findings {
		out[i] = f.Code.String() + " " + f.Message
	}
	return out
}

func TestResolveHidesLocalsBeyondTheHorizon(t *testing.T) {
	t.Parallel()

	ix := &Index{
		Symbols: []Symbol{
			{Name: "x", Kind: KindParameter, Span: span(2, 3), Selection: span(2, 3), Scope: 1},
			{Name: "y", Kind: KindParameter, Span: span(4, 7), Selection: span(4, 5), Scope: 1},
		},
	}
	m := &Module{Files: []*Index{ix}}
	if got := m.Resolve(0, Ref{Name: "x", Kind: KindVariable, Scope: 1, Horizon: 4}).Targets; len(got) != 1 {
		t.Errorf("an earlier parameter = %v, want it found", got)
	}
	if got := m.Resolve(0, Ref{Name: "y", Kind: KindVariable, Scope: 1, Horizon: 4}); got.Found() {
		t.Errorf("the parameter itself = %+v, want it hidden", got)
	}
}

func TestResolvePrefersTheLocalScope(t *testing.T) {
	t.Parallel()

	ix := &Index{
		Symbols: []Symbol{
			{Name: "x", Kind: KindVariable, Selection: span(0, 1)},
			{Name: "x", Kind: KindParameter, Selection: span(10, 11), Scope: 1},
		},
	}
	m := &Module{Files: []*Index{ix}}
	if got := m.Resolve(0, Ref{Name: "x", Kind: KindVariable, Scope: 1}).Targets; len(got) != 1 || got[0].Symbol != 1 {
		t.Errorf("x inside the recipe = %+v, want the parameter", got)
	}
	if got := m.Resolve(0, Ref{Name: "x", Kind: KindVariable, Scope: 2}).Targets; len(got) != 1 || got[0].Symbol != 0 {
		t.Errorf("x in another recipe = %+v, want the variable", got)
	}
}

func TestQualifiedReferencesEnterSubmodules(t *testing.T) {
	t.Parallel()

	child := &Module{Files: []*Index{{Symbols: []Symbol{{Name: "lint", Kind: KindRecipe}}}}}
	root := &Module{Files: []*Index{{}}, Submodules: map[string]*Module{"tools": child, "gone": nil}}
	if got := root.Resolve(0, Ref{Name: "lint", Kind: KindRecipe, Call: Qualified([]string{"tools"})}); len(got.Targets) != 1 || got.Targets[0].Module != child {
		t.Errorf("tools::lint = %+v", got)
	}
	if got := root.Resolve(0, Ref{Name: "lint", Kind: KindRecipe, Call: Qualified([]string{"gone"})}); !got.Missing {
		t.Errorf("gone::lint, from an optional module that is absent, = %+v, want missing", got)
	}
	if got := root.Resolve(0, Ref{Name: "lint", Kind: KindRecipe}); got.Found() {
		t.Errorf("unqualified lint reaches into the module: %+v", got)
	}
}

func TestBuiltinsSortAndRejectDuplicates(t *testing.T) {
	t.Parallel()

	b := NewBuiltins([]Builtin{{Name: "b", Kind: KindFunction}, {Name: "a", Kind: KindFunction}})
	if got := b.Of(KindFunction); got[0].Name != "a" || got[1].Name != "b" {
		t.Errorf("Of = %+v, want sorted", got)
	}
	if _, ok := b.Lookup(KindFunction, "b"); !ok {
		t.Error("Lookup(b) failed")
	}
	defer func() {
		if recover() == nil {
			t.Error("a builtin listed twice did not panic")
		}
	}()
	NewBuiltins([]Builtin{{Name: "a", Kind: KindSetting}, {Name: "a", Kind: KindSetting}})
}

func TestArity(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		a    Arity
		text string
		ok   []int
		bad  []int
	}{
		{Arity{Min: 0, Max: 0}, "0 arguments", []int{0}, []int{1}},
		{Arity{Min: 1, Max: 1}, "1 argument", []int{1}, []int{0, 2}},
		{Arity{Min: 1, Max: 3}, "1 to 3 arguments", []int{1, 3}, []int{0, 4}},
		{Arity{Min: 2, Max: Variadic}, "at least 2 arguments", []int{2, 9}, []int{1}},
		{NoArity, "", []int{0, 5}, nil},
	} {
		if tt.a.Known() && tt.a.String() != tt.text {
			t.Errorf("%+v.String() = %q, want %q", tt.a, tt.a.String(), tt.text)
		}
		for _, n := range tt.ok {
			if !tt.a.Accepts(n) {
				t.Errorf("%+v rejects %d", tt.a, n)
			}
		}
		for _, n := range tt.bad {
			if tt.a.Accepts(n) {
				t.Errorf("%+v accepts %d", tt.a, n)
			}
		}
	}
}

func TestFindings(t *testing.T) {
	t.Parallel()

	ix := &Index{
		Symbols: []Symbol{
			{Name: "a", Kind: KindRecipe, Selection: span(0, 1), Arity: Arity{Min: 0, Max: 0}},
			{Name: "b", Kind: KindRecipe, Selection: span(10, 11), Arity: Arity{Min: 1, Max: 1}},
			{Name: "unused", Kind: KindVariable, Selection: span(20, 26), Unused: true, Arity: NoArity},
			{Name: "used", Kind: KindVariable, Selection: span(30, 34), Unused: true, Arity: NoArity},
		},
		Refs: []Ref{
			{Name: "b", Kind: KindRecipe, Span: span(3, 4), Args: 0, Owner: 0, Edge: true},
			{Name: "a", Kind: KindRecipe, Span: span(13, 14), Args: 0, Owner: 1, Edge: true},
			{Name: "nope", Kind: KindRecipe, Span: span(40, 44), Args: 0, Owner: NoSymbol},
			{Name: "soft", Kind: KindRecipe, Span: span(45, 49), Args: NoArgs, Owner: NoSymbol, Soft: true},
			{Name: "used", Kind: KindVariable, Span: span(50, 54), Args: NoArgs, Owner: NoSymbol},
			{Name: "bogus", Kind: KindSetting, Span: span(60, 65), Args: NoArgs, Owner: NoSymbol},
		},
	}
	m := &Module{Files: []*Index{ix}, Complete: true}
	got := codes(m.Findings(0, testChecks, m.Usage()))
	want := []string{
		"SL5006 recipe `b` takes 1 argument but is given 0",
		"SL5002 unknown recipe `nope`",
		"SL5007 unknown setting `bogus`",
		"SL5012 recipe `a` is circular: `a -> b -> a`",
		"SL5012 recipe `b` is circular: `b -> a -> b`",
		"SL5014 variable `unused` is never used",
	}
	if !slices.Equal(got, want) {
		t.Errorf("findings\n got %q\nwant %q", got, want)
	}
	m.Complete = false
	got = codes(m.Findings(0, testChecks, m.Usage()))
	if slices.ContainsFunc(got, func(s string) bool { return strings.Contains(s, "nope") || strings.Contains(s, "never used") }) || !slices.ContainsFunc(got, func(s string) bool { return strings.Contains(s, "bogus") }) {
		t.Errorf("with an include missing, findings = %q; want unknown names and unused ones held back, but settings still checked", got)
	}
	ix.Broken = true
	m.Complete = true
	if got := codes(m.Findings(0, testChecks, m.Usage())); slices.ContainsFunc(got, func(s string) bool { return strings.Contains(s, "nope") }) {
		t.Errorf("a file that does not parse still reports unknown names: %q", got)
	}
}

func TestMissingInclude(t *testing.T) {
	t.Parallel()

	f := MissingInclude(testChecks, Include{Module: "tools", Candidates: []string{"tools.just", "tools/mod.just"}})
	if f.Code != lint.CodeJustMissingFile || f.Message != "no source file for module `tools`; looked for `tools.just`, `tools/mod.just`" {
		t.Errorf("module = %+v", f)
	}
	if f := MissingInclude(testChecks, Include{Path: "ci.just"}); f.Message != "`ci.just` is not found" {
		t.Errorf("import = %+v", f)
	}
}

func TestIndexLookups(t *testing.T) {
	t.Parallel()

	ix := &Index{
		Symbols: []Symbol{
			{Name: "r", Kind: KindRecipe, Selection: span(0, 1), Parent: NoParent},
			{Name: "p", Kind: KindParameter, Selection: span(2, 3), Parent: 0, Scope: 1, Exported: true},
		},
		Refs:   []Ref{{Name: "p", Kind: KindVariable, Span: span(8, 9), Scope: 1}},
		Shells: []Shell{{Span: span(5, 20), Scope: 1}},
	}
	if hit, ok := ix.At(2); !ok || !hit.Declaration || hit.Symbol != 1 {
		t.Errorf("At(2) = %+v", hit)
	}
	if hit, ok := ix.At(8); !ok || hit.Declaration || hit.Name != "p" {
		t.Errorf("At(8) = %+v", hit)
	}
	if scope, ok := ix.ScopeAt(10); !ok || scope != 1 {
		t.Errorf("ScopeAt(10) = %d, %v", scope, ok)
	}
	if _, ok := ix.ScopeAt(30); ok {
		t.Error("ScopeAt(30) is inside a shell")
	}
	if got := ix.Exported("p", 1); !slices.Equal(got, []int{1}) {
		t.Errorf("Exported = %v", got)
	}
	if got := ix.Children(0); !slices.Equal(got, []int32{1}) {
		t.Errorf("Children = %v", got)
	}
}

func TestCyclesAreFoundOnlyOnTheCycle(t *testing.T) {
	refTo := func(name string, owner int) Ref {
		return Ref{Name: name, Kind: KindFunction, Owner: Narrow(owner), Edge: true, Args: NoArgs, Span: Span{Start: owner, End: owner + 1}}
	}
	symbol := func(name string, at int) Symbol {
		return Symbol{Name: name, Kind: KindFunction, Span: Span{Start: at, End: at + 1}, Selection: Span{Start: at, End: at + 1}, Parent: NoParent, Arity: NoArity}
	}
	ix := &Index{
		Symbols: []Symbol{symbol("loopA", 0), symbol("loopB", 1), symbol("selfish", 2), symbol("leaf", 3), symbol("tail", 4)},
		Refs:    []Ref{refTo("loopB", 0), refTo("loopA", 1), refTo("selfish", 2), refTo("loopA", 4)},
	}
	m := &Module{Files: []*Index{ix}}
	search := newCycleSearch(m)
	whenever := []context{{at: whenever}}
	for i, want := range []bool{true, true, true, false, false} {
		_, _, found := search.cycle(node{0, i}, whenever)
		if found != want {
			t.Errorf("%s: on a cycle = %v, want %v", ix.Symbols[i].Name, found, want)
		}
	}
}

func TestARefStaysSmall(t *testing.T) {
	t.Parallel()

	const budget = 88
	if size := unsafe.Sizeof(Ref{}); size > budget {
		t.Errorf("a Ref takes %d bytes, over the budget of %d: an index holds one per read", size, budget)
	}
}

func TestLookupTablesKeepDeclarationOrder(t *testing.T) {
	t.Parallel()

	ix := &Index{
		Symbols: []Symbol{
			{Name: "b", Kind: KindVariable, Parent: NoParent},
			{Name: "a", Kind: KindFunction, Parent: 0},
			{Name: "a", Kind: KindVariable, Parent: 0},
			{Name: "b", Kind: KindVariable, Parent: 2, Hidden: true},
			{Name: "a", Kind: KindVariable, Parent: NoParent},
		},
		Refs: []Ref{
			{Name: "x", Owner: 2, Edge: true},
			{Name: "y", Owner: 0, Edge: true},
			{Name: "z", Owner: 2},
			{Name: "w", Owner: 2, Edge: true},
		},
	}
	for _, c := range []struct {
		got, want []int32
		what      string
	}{
		{ix.named(NamespaceValues, "a"), []int32{2, 4}, "values named a"},
		{ix.named(NamespaceValues, "b"), []int32{0, 3}, "values named b"},
		{ix.named(NamespaceFunctions, "a"), []int32{1}, "functions named a"},
		{ix.named(NamespaceFunctions, "b"), nil, "functions named b"},
		{ix.named(NamespaceValues, ""), nil, "values with no name"},
		{ix.Children(NoParent), []int32{0, 4}, "top level"},
		{ix.Children(0), []int32{1, 2}, "children of 0"},
		{ix.Children(2), nil, "children of 2, which are hidden"},
		{ix.edgesOf(2), []int32{0, 3}, "edges of 2"},
		{ix.edgesOf(1), nil, "edges of 1"},
	} {
		if !slices.Equal(c.got, c.want) {
			t.Errorf("%s = %v, want %v", c.what, c.got, c.want)
		}
	}
}

func TestNamesListPlacesInFileOrder(t *testing.T) {
	t.Parallel()

	m := &Module{Files: []*Index{
		{Symbols: []Symbol{{Name: "a"}, {Name: "b"}}, Refs: []Ref{{Name: "b"}}},
		{Symbols: []Symbol{{Name: "a"}}, Refs: []Ref{{Name: "c"}, {Name: "b"}}},
	}}
	if got, want := m.SymbolsNamed("a"), []Place{{File: 0, Item: 0}, {File: 1, Item: 0}}; !slices.Equal(got, want) {
		t.Errorf("SymbolsNamed(a) = %v, want %v", got, want)
	}
	if got, want := m.RefsNamed("b"), []Place{{File: 0, Item: 0}, {File: 1, Item: 1}}; !slices.Equal(got, want) {
		t.Errorf("RefsNamed(b) = %v, want %v", got, want)
	}
	if got := m.SymbolsNamed("missing"); len(got) != 0 {
		t.Errorf("SymbolsNamed(missing) = %v", got)
	}
}

func TestNamedFindsEveryNameThroughItsHash(t *testing.T) {
	t.Parallel()

	ix := &Index{}
	const count = 1000
	for i := range count {
		ix.Symbols = append(ix.Symbols, Symbol{Name: "name" + strconv.Itoa(i%300), Kind: KindVariable})
	}
	ix.Symbols = append(ix.Symbols, Symbol{Name: "name1", Kind: KindFunction})
	seen := 0
	for i := range 300 {
		run := ix.named(NamespaceValues, "name"+strconv.Itoa(i))
		seen += len(run)
		for at, item := range run {
			if ix.Symbols[item].Name != "name"+strconv.Itoa(i) || at > 0 && run[at-1] >= item {
				t.Fatalf("name%d lists %v", i, run)
			}
		}
	}
	if seen != count {
		t.Errorf("found %d values, want %d", seen, count)
	}
	if got := ix.named(NamespaceFunctions, "name1"); !slices.Equal(got, []int32{count}) {
		t.Errorf("functions named name1 = %v", got)
	}
	if got := ix.named(NamespaceValues, "absent"); got != nil {
		t.Errorf("an absent name lists %v", got)
	}
	if got := (&Index{}).named(NamespaceValues, "any"); got != nil {
		t.Errorf("an empty index lists %v", got)
	}
}
