package parser

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

func TestKshParsesKsh93UnlessTheScriptIsMksh(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		src  string
		lang syntax.LangVariant
		ok   bool
	}{
		{"#!/bin/ksh\nprint -r -- \"${.sh.version}\"\n", syntax.LangKsh93, true},
		{"#!/bin/ksh\nprint -r -- ${|REPLY=1;}\n", syntax.LangKsh93, false},
		{"#!/bin/mksh\nprint -r -- ${|REPLY=1;}\n", syntax.LangMirBSDKorn, true},
		{"#!/bin/mksh\nprint -r -- \"${.sh.version}\"\n", syntax.LangMirBSDKorn, false},
	} {
		tree := Parse(tc.src, dialect.Ksh)
		if tree.Lang != tc.lang || (len(tree.Errors) == 0) != tc.ok {
			t.Errorf("Parse(%q) = %s with errors %v, want %s parsing ok=%t", tc.src, tree.Lang, messages(tree), tc.lang, tc.ok)
		}
	}
}

func TestKsh93CompoundVariablesAreIndexedAsOneVariable(t *testing.T) {
	t.Parallel()

	src := "point=(x=1 y=(z=2))\nprint -r -- ${point.x} ${point.y.z}\ntypeset -T Pt_t=(integer len=0; get() { :; })\n"
	tree := Parse(src, dialect.Ksh)
	if len(tree.Errors) != 0 {
		t.Fatalf("errors = %v, want none", messages(tree))
	}
	defs, commands, _ := indexedNames(tree)
	if !slices.Equal(defs, []string{"point"}) {
		t.Errorf("definitions = %v, want only the compound variable and not the type", defs)
	}
	if !slices.Equal(commands, []string{"print", "typeset", "integer", ":"}) {
		t.Errorf("commands = %v, want print, typeset and the commands inside the type", commands)
	}
	if len(tree.Functions) != 0 {
		t.Errorf("functions = %+v, want the type's discipline left out", tree.Functions)
	}
	if attrs := tree.Definitions[0].Attrs; attrs.Has(AttrArray) {
		t.Errorf("point attrs = %06b, want a compound variable rather than an array", attrs)
	}
	var refs []string
	for _, r := range tree.References {
		refs = append(refs, src[r.Span.Start:r.Span.End])
	}
	if !slices.Equal(refs, []string{"point", "point"}) {
		t.Errorf("references = %q, want the compound variable twice", refs)
	}
}

func TestKsh93ArithmeticFunctionParametersAreLocalNamerefs(t *testing.T) {
	t.Parallel()

	src := "function .sh.math.hypot x y {\n  .sh.value=$(( sqrt(x*x + y*y) ))\n}\nnamespace geo { print $(( hypot(3, 4) )); }\n"
	tree := Parse(src, dialect.Ksh)
	if len(tree.Errors) != 0 {
		t.Fatalf("errors = %v, want none", messages(tree))
	}
	var params []string
	for _, d := range tree.Definitions {
		if d.Local && d.Attrs.Has(AttrNameref) && d.Within != FileScope {
			params = append(params, d.Name)
		}
	}
	if !slices.Equal(params, []string{"x", "y"}) {
		t.Errorf("local namerefs = %v, want x and y", params)
	}
	_, _, keywords := indexedNames(tree)
	if !slices.Equal(keywords, []string{"function", "namespace"}) {
		t.Errorf("keywords = %v, want function and namespace", keywords)
	}
}

func TestKsh93NamespacesAndTypesShapeTheIndex(t *testing.T) {
	t.Parallel()

	src := "enum Color_t=(red green)\nColor_t shade=red\ntypeset -F2 price=1.5\nnamespace shop {\n  function add { :; }\n}\nbox=(w=1)\nbox.w=2\n"
	tree := Parse(src, dialect.Ksh)
	if len(tree.Errors) != 0 {
		t.Fatalf("errors = %v, want none", messages(tree))
	}
	defs, _, _ := indexedNames(tree)
	if !slices.Equal(defs, []string{"shade", "price", "box"}) {
		t.Errorf("definitions = %v, want the enum instance, the float and the compound variable", defs)
	}
	if len(tree.Functions) != 1 || tree.Functions[0].Name != "add" || tree.Functions[0].Namespace != "shop" {
		t.Errorf("functions = %+v, want add in the shop name space", tree.Functions)
	}
	var refs []string
	for _, r := range tree.References {
		refs = append(refs, r.Name)
	}
	if !slices.Equal(refs, []string{"box"}) {
		t.Errorf("references = %v, want the member assignment to reference box", refs)
	}
}

func TestTypesetFloatFlagsDeclareVariablesOutsideBash(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		d    dialect.Dialect
		want []string
	}{
		{dialect.Bash, nil},
		{dialect.Zsh, []string{"pi"}},
		{dialect.Ksh, []string{"pi"}},
	} {
		defs, _, _ := indexedNames(Parse("typeset -F pi=3.14\n", tc.d))
		if !slices.Equal(defs, tc.want) {
			t.Errorf("%s definitions = %v, want %v", tc.d, defs, tc.want)
		}
	}
}
