package scope

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

func occurrence(src, name string, at int) int {
	word := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
	for i, m := range word.FindAllStringIndex(src, -1) {
		if m[0] >= at {
			return i
		}
	}
	return -1
}

func rendered(src string, u Use) string {
	use := fmt.Sprintf("%s%d=", u.Name, occurrence(src, u.Name, u.Node.Span().Start))
	switch u.Ref.Kind {
	case Definition:
		return use + fmt.Sprintf("%s%d", u.Name, occurrence(src, u.Name, u.Ref.Def.Binders[0].Span().Start))
	case Global:
		return use + "global"
	case Dynamic:
		return use + "with"
	case Undefined:
	}
	return use + "undefined"
}

func TestResolve(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src string
		want      []string
	}{
		{"rec sees its own attributes", `let x = 1; in rec { x = 2; y = x; }`, []string{"x2=x1"}},
		{"a plain set does not", `let x = 1; in { x = 2; y = x; }`, []string{"x2=x0"}},
		{"inherit in rec looks outward", `let x = 1; in rec { inherit x; y = x; }`, []string{"x1=x0", "x2=x1"}},
		{"inherit in let looks outward", `let x = 1; in let inherit x; in x`, []string{"x1=x0", "x2=x1"}},
		{"inherit never finds itself", `let inherit x; in x`, []string{"x0=undefined", "x1=x0"}},
		{"inherit from reads inside rec", `let e = 1; in rec { e = 2; inherit (e) y; }`, []string{"e2=e1"}},
		{"inherit from reads outside a plain set", `let e = 1; in { e = 2; inherit (e) y; }`, []string{"e2=e0"}},
		{"inherit from reads inside let", `let e = 1; inherit (e) y; in y`, []string{"e1=e0", "y1=y0"}},
		{"let bindings see one another", `let a = b; b = 1; in a`, []string{"b0=b1", "a1=a0"}},
		{"defaults see other formals", `{a, b ? a}: b`, []string{"a1=a0", "b1=b0"}},
		{"defaults see the alias", `args@{a ? args}: a`, []string{"args1=args0", "a1=a0"}},
		{"defaults see a later formal", `{a ? b, b}: a`, []string{"b0=b1", "a1=a0"}},
		{"a lexical name beats an inner with", `x: with z; x`, []string{"z0=undefined", "x1=x0"}},
		{"a with is not lexical for its own scope", `with a; 1`, []string{"a0=undefined"}},
		{"a with resolves the rest", `y: with y; b`, []string{"y1=y0", "b0=with"}},
		{"nested withs", `with a; with b; c`, []string{"a0=undefined", "b0=with", "c0=with"}},
		{"globals beat a with", `y: with y; map`, []string{"y1=y0", "map0=global"}},
		{"true is an ordinary global", `y: with y; true`, []string{"y1=y0", "true0=global"}},
		{"a let can shadow a global", `let true = 1; in true`, []string{"true1=true0"}},
		{"curPos ignores a let", `let __curPos = 1; in __curPos`, []string{"__curPos1=global"}},
		{"curPos ignores a formal", `{__curPos}: __curPos`, []string{"__curPos1=global"}},
		{"curPos cannot be inherited", `let inherit __curPos; in 1`, []string{"__curPos0=undefined"}},
		{"inheriting curPos can come from a with", `y: with y; { inherit __curPos; }`, []string{"y1=y0", "__curPos0=with"}},
		{"hidden aliases of builtins", `__attrNames`, []string{"__attrNames0=global"}},
		{"globals have no hidden alias", `__map`, []string{"__map0=undefined"}},
		{"unknown names", `__zzz`, []string{"__zzz0=undefined"}},
		{"nested names of rec stay nested", `rec { a.b = 1; c = a; d = b; }`, []string{"a1=a0", "b1=undefined"}},
		{"a later set joins an earlier rec", `{ a = rec { b = 1; }; a = { c = b; }; }`, []string{"b1=b0"}},
		{"a dotted path joins an earlier rec", `{ a = rec { b = 1; }; a.c = b; }`, []string{"b1=b0"}},
		{"a later rec does not bind an earlier plain set", `{ a = { b = 1; }; a = rec { c = b; }; }`, []string{"b1=undefined"}},
		{"a later rec does not bind an implicit set", `{ a.b = 1; a = rec { c = b; }; }`, []string{"b1=undefined"}},
		{"inherit from in a joined set reads the rec", `{ a = rec { x = 1; }; a = { inherit (x) y; }; }`, []string{"x1=x0"}},
		{"inherit in a joined set looks outward", `let v = 1; in { c = rec { u = 2; }; c = { inherit v; }; }`, []string{"v1=v0"}},
		{"a dynamic key in a joined set reads the rec", `let x = 1; in { a = rec { x = 2; }; a.${x} = 3; }`, []string{"x2=x1"}},
		{"a constant dynamic key binds", `let ${"a"} = 1; in a`, []string{"a1=a0"}},
		{"a constant dynamic key binds in rec", `rec { ${"a"} = 1; b = a; }`, []string{"a1=a0"}},
		{"a quoted key binds", `let "a" = 1; in a`, []string{"a1=a0"}},
		{"an escaped key binds", `let "\a" = 1; in a`, []string{"a1=a0"}},
		{"a real dynamic key binds nothing", `let x = 1; in rec { ${x} = 2; b = k; }`, []string{"x1=x0", "k0=undefined"}},
		{"overrides add no names", `rec { __overrides = { }; a = b; }`, []string{"b0=undefined"}},
		{"legacy let", `let { a = 1; body = a; }`, []string{"a1=a0"}},
		{"or as a variable", `let or = 1; f = 2; in f or`, []string{"f1=f0", "or1=or0"}},
		{"a repeated formal is one name", `{a, a}: a`, []string{"a2=a0"}},
		{"string interpolation", `x: "${x}"`, []string{"x1=x0"}},
		{"path interpolation", `x: ./a/${x}`, []string{"x1=x0"}},
		{"inner lambda shadows", `x: x: x`, []string{"x2=x1"}},
		{"dynamic select", `x: a.${x}`, []string{"a0=undefined", "x1=x0"}},
		{"dynamic has-attr", `x: a ? ${x}`, []string{"a0=undefined", "x1=x0"}},
		{"dynamic steps of a path", `let x = 1; in { a.${x}.b = x; }`, []string{"x1=x0", "x2=x0"}},
		{"defaults after or", `x: x.a or x`, []string{"x1=x0", "x2=x0"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			table := Resolve(syntax.Parse(tt.src))
			var got []string
			for _, u := range table.Uses() {
				got = append(got, rendered(tt.src, u))
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("%s resolves to %q, want %q", tt.src, got, tt.want)
			}
		})
	}
}

func TestDuplicates(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		src  string
		want []string
	}{
		{`{ a = 1; a = 2; }`, []string{"a"}},
		{`{ a.b = 1; a.c = 2; }`, nil},
		{`{ a.b = 1; a.b = 2; }`, []string{"a.b"}},
		{`{ a = { b = 1; }; a.c = 2; }`, nil},
		{`{ a.c = 2; a = { b = 1; }; }`, nil},
		{`{ a = { b = 1; }; a = { c = 2; }; }`, nil},
		{`{ a = { b.c = 1; }; a = { b.d = 2; }; }`, nil},
		{`{ a = { b = 1; }; a = { b = 2; }; }`, []string{"a.b"}},
		{`{ a = { b = { c = 1; }; }; a = { b = { d = 2; }; }; }`, nil},
		{`{ a = { b = { c = 1; }; }; a = { b = { c = 2; }; }; }`, []string{"a.b.c"}},
		{`{ a = ({ b = 1; }); a.c = 2; }`, nil},
		{`{ a = ({ b = 1; }); a = ({ c = 2; }); }`, nil},
		{`{ a = 1; a.b = 2; }`, []string{"a"}},
		{`{ a.b = 2; a = 1; }`, []string{"a"}},
		{`{ a.b = 1; a = { b.c = 2; }; }`, []string{"a.b"}},
		{`{ a = { b = 1; }; a.b.c = 2; }`, []string{"a.b"}},
		{`{ inherit a; inherit a; }`, []string{"a"}},
		{`{ inherit a; a = 1; }`, []string{"a"}},
		{`{ inherit a; a.b = 1; }`, []string{"a"}},
		{`{ inherit a "a"; }`, []string{"a"}},
		{`{ "a" = 1; a = 2; }`, []string{"a"}},
		{`{ "a".b = 1; a.c = 2; }`, nil},
		{`{ ${"a"} = 1; a = 2; }`, []string{"a"}},
		{`{ ${x} = 1; ${x} = 2; }`, nil},
		{`{ ${x}.a = 1; ${x}.a = 2; }`, nil},
		{`{ a = x; a = { }; }`, []string{"a"}},
		{`let a.b = 1; a.c = 2; in a`, nil},
		{`let a = { b = 1; }; a.c = 2; in a`, nil},
		{`let a = 1; a = 2; in a`, []string{"a"}},
		{`let { body = 1; body = 2; }`, []string{"body"}},
		{`x: { a = rec { y = 2; }; a = { inherit y; }; }`, []string{"a.y"}},
	} {
		var got []string
		for _, d := range Resolve(syntax.Parse(tt.src)).Duplicates() {
			got = append(got, d.Path)
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("%s has the duplicates %q, want %q", tt.src, got, tt.want)
		}
	}
}

func TestDefinitionsAndTheirUses(t *testing.T) {
	t.Parallel()

	src := `let a.b = 1; a.c = 2; f = x: a; g = f; in f g`
	table := Resolve(syntax.Parse(src))
	kinds := map[string]DefKind{}
	uses := map[string]int{}
	for _, d := range table.Defs() {
		kinds[d.Name] = d.Kind
		uses[d.Name] = len(d.Uses())
	}
	if kinds["a"] != LetDef || kinds["x"] != ArgDef || uses["a"] != 1 || uses["f"] != 2 || uses["g"] != 1 || uses["x"] != 0 {
		t.Errorf("kinds %v, uses %v", kinds, uses)
	}
	a := table.Defs()[0]
	if a.Name != "a" || len(a.Binders) != 2 || len(a.Bindings) != 2 {
		t.Errorf("the merged definition of a is %+v", a)
	}
	if a.Uses()[0].Node.Span().Start != strings.Index(src, ": a;")+2 {
		t.Errorf("a is used at %+v", a.Uses()[0].Node.Span())
	}
}

func TestDefKinds(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		src, name string
		want      DefKind
	}{
		{`let a = 1; in a`, "a", LetDef},
		{`x: let inherit (x) a; in a`, "a", InheritDef},
		{`rec { a = 1; b = a; }`, "a", RecDef},
		{`x: rec { inherit x; b = x; }`, "b", RecDef},
		{`x: x`, "x", ArgDef},
		{`{ x }: x`, "x", FormalDef},
		{`x@{ }: x`, "x", AliasDef},
		{`{ }@x: x`, "x", AliasDef},
	} {
		table := Resolve(syntax.Parse(tt.src))
		i := slices.IndexFunc(table.Defs(), func(d *Def) bool { return d.Name == tt.name })
		if i < 0 || table.Defs()[i].Kind != tt.want {
			t.Errorf("%s: %s is not of kind %d in %+v", tt.src, tt.name, tt.want, table.Defs())
		}
	}
}

func TestImplicitReaders(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		src, name string
		implicit  bool
	}{
		{`let __sub = 1; in 2 - 3`, "__sub", true},
		{`let __lessThan = 1; in 2 < 3`, "__lessThan", true},
		{`let __findFile = 1; in <a>`, "__findFile", true},
		{`rec { __overrides = { }; }`, "__overrides", true},
		{`let __overrides = 1; in 2`, "__overrides", false},
		{`let { __overrides = { }; body = 1; }`, "__overrides", true},
		{`let { body = 1; }`, "body", true},
		{`let body = 1; in 2`, "body", false},
		{`let __add = 1; in 2 + 3`, "__add", false},
		{`let __curPos = 1; in 2`, "__curPos", false},
	} {
		table := Resolve(syntax.Parse(tt.src))
		i := slices.IndexFunc(table.Defs(), func(d *Def) bool { return d.Name == tt.name })
		if i < 0 || table.Defs()[i].Implicit != tt.implicit {
			t.Errorf("%s: implicit %v wanted for %s in %+v", tt.src, tt.implicit, tt.name, table.Defs())
		}
	}
}

func TestBinders(t *testing.T) {
	t.Parallel()

	set := syntax.Parse(`x: rec { inherit x; a = x; }`)
	table := Resolve(set)
	lambda := set.Root.(*syntax.Lambda)
	param := lambda.Param.(*syntax.IdentParam)
	arg, ok := table.Binder(param.Name)
	if !ok || arg.Kind != ArgDef {
		t.Fatalf("the argument binds %+v", arg)
	}
	if ref, known := table.Of(param.Name); !known || ref.Def != arg {
		t.Errorf("the argument's own identifier resolves to %+v", ref)
	}
	body := lambda.Body.(*syntax.AttrSet)
	attr := body.Bindings[0].(*syntax.Inherit).Attrs[0]
	inherited, ok := table.Binder(attr)
	if !ok || inherited.Kind != InheritDef {
		t.Fatalf("the inherit binds %+v", inherited)
	}
	if outward, known := table.Inherited(attr); !known || outward.Def != arg {
		t.Errorf("the inherit reads %+v, want the argument", outward)
	}
	value := body.Bindings[1].(*syntax.Assign).Value.(*syntax.Ident)
	if ref, known := table.Of(value); !known || ref.Def != inherited {
		t.Errorf("the value reads %+v, want the inherited name", ref)
	}
	if s, owned := table.ScopeOf(body); !owned || s.Kind != RecScope || s.Parent.Kind != LambdaScope {
		t.Errorf("the rec set's scope is %+v", s)
	}
}

func TestWiths(t *testing.T) {
	t.Parallel()

	f := syntax.Parse(`x: with x; with x; y`)
	table := Resolve(f)
	outer := f.Root.(*syntax.Lambda).Body.(*syntax.With)
	inner := outer.Body.(*syntax.With)
	ref, ok := table.Of(inner.Body.(*syntax.Ident))
	if !ok || ref.Kind != Dynamic || ref.With != inner {
		t.Fatalf("y resolves to %+v", ref)
	}
	if got := table.Withs(ref.With); !slices.Equal(got, []*syntax.With{inner, outer}) {
		t.Errorf("Withs = %+v", got)
	}
}

func TestNamesAt(t *testing.T) {
	t.Parallel()

	lexical := func(n Names) []string {
		var out []string
		for _, name := range n.Visible {
			if name.Ref.Kind == Definition {
				out = append(out, name.Name)
			}
		}
		return out
	}
	for _, tt := range []struct {
		name, src, marker string
		want              []string
		withs             int
	}{
		{"file level", `a`, "a", nil, 0},
		{"let binding", `let a = b; b = 1; in a`, "b;", []string{"a", "b"}, 0},
		{"let body", `let a = 1; in zz`, "zz", []string{"a"}, 0},
		{"plain set", `let a = 1; in { b = zz; }`, "zz", []string{"a"}, 0},
		{"rec set", `let a = 1; in rec { b = zz; }`, "zz", []string{"a", "b"}, 0},
		{"lambda", `x: { y, z ? zz }: 1`, "zz", []string{"x", "y", "z"}, 0},
		{"shadowed once", `x: x: zz`, "zz", []string{"x"}, 0},
		{"with", `x: with x; zz`, "zz", []string{"x"}, 1},
		{"with scope", `x: with zz; 1`, "zz", []string{"x"}, 0},
		{"nested with", `x: with x; with x; zz`, "zz", []string{"x"}, 2},
		{"inherit names skip the let", `let a = 1; in let c = 2; inherit zz; in c`, "zz", []string{"a"}, 0},
		{"inherit from is inside", `let c = 2; inherit (zz) y; in c`, "zz", []string{"c", "y"}, 0},
		{"inherit from names skip the let", `let c = 2; inherit (c) zz; in c`, "zz", nil, 0},
		{"joined rec", `{ a = rec { b = 1; }; a = { c = zz; }; }`, "zz", []string{"b", "c"}, 0},
		{"dotted into a rec", `{ a = rec { b = 1; }; a.c = zz; }`, "zz", []string{"b", "c"}, 0},
		{"after a scope closed", `(x: 1) zz`, "zz", nil, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			table := Resolve(syntax.Parse(tt.src))
			names := table.NamesAt(strings.LastIndex(tt.src, tt.marker))
			if got := lexical(names); !slices.Equal(got, tt.want) {
				t.Errorf("%s has the names %q, want %q", tt.src, got, tt.want)
			}
			if len(names.Withs) != tt.withs {
				t.Errorf("%s has %d withs, want %d", tt.src, len(names.Withs), tt.withs)
			}
		})
	}
	src := `let map = 1; __curPos = 2; in map`
	kinds := map[string]RefKind{}
	for _, n := range Resolve(syntax.Parse(src)).NamesAt(strings.LastIndex(src, "map")).Visible {
		kinds[n.Name] = n.Ref.Kind
	}
	if kinds["map"] != Definition || kinds["toString"] != Global || kinds["__curPos"] != Global {
		t.Errorf("the names inside the let are %v", kinds)
	}
}

func TestLongDottedPathsDoNotRecurse(t *testing.T) {
	t.Parallel()

	const depth = 200000
	path := strings.Repeat("a.", depth) + "a"
	src := "x: { " + path + " = x; b = { " + path + " = 1; }; b = { " + path + " = 2; }; }"
	table := Resolve(syntax.Parse(src))
	if got := table.Duplicates(); len(got) != 1 || !strings.HasPrefix(got[0].Path, "b.a.a.") {
		t.Errorf("the deep paths have %d duplicates", len(got))
	}
	if uses := table.Defs()[0].Uses(); len(uses) != 1 {
		t.Errorf("x has %d uses", len(uses))
	}
}

func BenchmarkResolveShadowedScopes(b *testing.B) {
	var src strings.Builder
	for range 1000 {
		src.WriteString("x: ")
	}
	for range 1000 {
		src.WriteString("x + ")
	}
	src.WriteString("x")
	f := syntax.Parse(src.String())
	for b.Loop() {
		Resolve(f)
	}
}

func TestCapture(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src, newName string
		found              bool
		kind               CaptureKind
	}{
		{name: "free", src: "let one = 1; in one", newName: "two"},
		{name: "same scope", src: "let one = 1; two = 2; in one", newName: "two", found: true, kind: AlreadyBound},
		{name: "a lambda around a read", src: "let one = 1; in two: one", newName: "two", found: true, kind: CapturedRead},
		{name: "a let around a read", src: "let one = 1; in let two = 2; in one", newName: "two", found: true, kind: CapturedRead},
		{name: "a let around a read in an inherit", src: "let one = 1; in let two = 2; inherit one; in two", newName: "two", found: true, kind: CapturedRead},
		{name: "a binding beside a read", src: "let one = 1; in [ one (two: two) ]", newName: "two"},
		{name: "a read of an outer binding", src: "let two = 2; in let one = 1; in one + two", newName: "two", found: true, kind: ShadowedRead},
		{name: "a read of a builtin", src: "let one = 1; in map", newName: "map", found: true, kind: ShadowedRead},
		{name: "a read through with", src: "let one = 1; in with s; two", newName: "two", found: true, kind: ShadowedRead},
		{name: "a read outside", src: "[ two (let one = 1; in one) ]", newName: "two"},
		{name: "a read with its own binding inside", src: "let one = 1; in two: two", newName: "two"},
		{name: "an inherit reads past the let", src: "let two = 2; in let one = 1; inherit two; in one", newName: "two", found: true, kind: AlreadyBound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			table := Resolve(syntax.Parse(tt.src))
			i := slices.IndexFunc(table.Defs(), func(d *Def) bool { return d.Name == "one" })
			if i < 0 {
				t.Fatalf("no binding of one in %q", tt.src)
			}
			got, found := table.Capture(table.Defs()[i], tt.newName)
			if found != tt.found || found && got.Kind != tt.kind {
				t.Errorf("Capture = %+v, %v, want kind %d, %v", got, found, tt.kind, tt.found)
			}
		})
	}
}

func TestReserved(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]bool{"__curPos": true, "__overrides": true, "__sub": true, "__findFile": true, "body": false, "x": false} {
		if got := Reserved(name); got != want {
			t.Errorf("Reserved(%q) = %v, want %v", name, got, want)
		}
	}
}
