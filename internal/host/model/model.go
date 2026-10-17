package model

import (
	"fmt"
	"iter"
	"slices"
	"sort"

	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/semantic"
)

type Span = parser.Span

type Kind uint8

const (
	KindRecipe Kind = iota
	KindAlias
	KindVariable
	KindParameter
	KindFunction
	KindModule
	KindTarget
	KindSetting
	KindAttribute
	KindKeyword
)

var kindNames = [...]string{
	KindRecipe:    "recipe",
	KindAlias:     "alias",
	KindVariable:  "variable",
	KindParameter: "parameter",
	KindFunction:  "function",
	KindModule:    "module",
	KindTarget:    "target",
	KindSetting:   "setting",
	KindAttribute: "attribute",
	KindKeyword:   "keyword",
}

func Kinds() iter.Seq[Kind] {
	return func(yield func(Kind) bool) {
		for k := range Kind(len(kindNames)) {
			if !yield(k) {
				return
			}
		}
	}
}

func (k Kind) String() string {
	if int(k) >= len(kindNames) {
		panic(fmt.Sprintf("model: unknown kind %d", k))
	}
	return kindNames[k]
}

type Namespace uint8

const (
	NamespaceRecipes Namespace = iota
	NamespaceValues
	NamespaceFunctions
	NamespaceModules
	NamespaceTargets
	NamespaceSettings
	NamespaceAttributes
	NamespaceKeywords
)

func (k Kind) Namespace() Namespace {
	switch k {
	case KindRecipe, KindAlias:
		return NamespaceRecipes
	case KindVariable, KindParameter:
		return NamespaceValues
	case KindFunction:
		return NamespaceFunctions
	case KindModule:
		return NamespaceModules
	case KindTarget:
		return NamespaceTargets
	case KindSetting:
		return NamespaceSettings
	case KindAttribute:
		return NamespaceAttributes
	case KindKeyword:
		return NamespaceKeywords
	}
	panic(fmt.Sprintf("model: kind %d has no namespace", k))
}

type Scope int

const Global Scope = 0

type Symbol struct {
	Name      string
	Kind      Kind
	Span      Span
	Selection Span
	Scope     Scope
	Parent    int
	Detail    string
	Doc       string
	Exported  bool
	Unused    bool
	Arity     Arity
}

const NoParent = -1

type Arity struct {
	Min, Max int
}

var NoArity = Arity{Min: -1, Max: -1}

func (a Arity) Known() bool { return a.Min >= 0 }

func (a Arity) Accepts(n int) bool {
	return !a.Known() || n >= a.Min && (a.Max == Variadic || n <= a.Max)
}

type Ref struct {
	Name      string
	Kind      Kind
	Span      Span
	Scope     Scope
	Qualifier []string
	Args      int
	Owner     int
	Edge      bool
	Soft      bool
}

const NoArgs = -1

func (r Ref) Qualified() bool { return len(r.Qualifier) > 0 }

type Include struct {
	Span       Span
	Path       string
	Candidates []string
	Optional   bool
	Module     string
	Dynamic    bool
}

type Shell struct {
	Span  Span
	Scope Scope
}

type Fold struct {
	Span    Span
	Comment bool
}

type Runnable struct {
	Name string
	Span Span
}

type Index struct {
	Symbols   []Symbol
	Refs      []Ref
	Includes  []Include
	Shells    []Shell
	Folds     []Fold
	Tokens    []semantic.Token
	Runnables []Runnable
	Findings  []lint.Finding
	ExportAll bool
	Broken    bool
}

type Certainty uint8

const (
	WhenComplete Certainty = iota
	Regardless
)

type Rules struct {
	Unknown     lint.Code
	UnknownWhen Certainty
	Arity       lint.Code
	Circular    lint.Code
	Unused      lint.Code
}

type Checks struct {
	Of         func(Kind) Rules
	Deprecated lint.Code
	Missing    lint.Code
}

type Expect struct {
	Kinds []Kind
	Span  Span
	Scope Scope
}

type Builtin struct {
	Name       string
	Kind       Kind
	Signature  string
	Doc        string
	Arity      Arity
	Deprecated string
}

const Variadic = -1

type Builtins struct {
	byKind map[Kind][]Builtin
}

func NewBuiltins(entries ...[]Builtin) Builtins {
	b := Builtins{byKind: map[Kind][]Builtin{}}
	for _, list := range entries {
		for _, e := range list {
			b.byKind[e.Kind] = append(b.byKind[e.Kind], e)
		}
	}
	for kind, list := range b.byKind {
		slices.SortFunc(list, func(a, b Builtin) int { return compareNames(a.Name, b.Name) })
		for i := 1; i < len(list); i++ {
			if list[i-1].Name == list[i].Name {
				panic(fmt.Sprintf("model: builtin %s %q is listed twice", kind, list[i].Name))
			}
		}
	}
	return b
}

func compareNames(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func (b Builtins) Of(kind Kind) []Builtin { return b.byKind[kind] }

func (b Builtins) Lookup(kind Kind, name string) (Builtin, bool) {
	list := b.byKind[kind]
	i, found := slices.BinarySearchFunc(list, name, func(e Builtin, name string) int { return compareNames(e.Name, name) })
	if !found {
		return Builtin{}, false
	}
	return list[i], true
}

type Hit struct {
	Name        string
	Kind        Kind
	Span        Span
	Scope       Scope
	Qualifier   []string
	Symbol      int
	Declaration bool
}

const NoSymbol = -1

func (ix *Index) At(offset int) (Hit, bool) {
	for i, s := range ix.Symbols {
		if s.Selection.Touches(offset) && s.Selection.End > s.Selection.Start {
			return Hit{Name: s.Name, Kind: s.Kind, Span: s.Selection, Scope: s.Scope, Symbol: i, Declaration: true}, true
		}
	}
	for _, r := range ix.Refs {
		if r.Span.Touches(offset) && r.Span.End > r.Span.Start {
			return Hit{Name: r.Name, Kind: r.Kind, Span: r.Span, Scope: r.Scope, Qualifier: r.Qualifier, Symbol: NoSymbol}, true
		}
	}
	return Hit{}, false
}

func (ix *Index) Lookup(name string, ns Namespace, scope Scope) []int {
	var local, global []int
	for i, s := range ix.Symbols {
		if s.Name != name || s.Kind.Namespace() != ns {
			continue
		}
		switch s.Scope {
		case scope:
			if scope != Global {
				local = append(local, i)
				continue
			}
			global = append(global, i)
		case Global:
			global = append(global, i)
		}
	}
	if len(local) > 0 {
		return local
	}
	return global
}

func (ix *Index) ScopeAt(offset int) (Scope, bool) {
	i := sort.Search(len(ix.Shells), func(i int) bool { return ix.Shells[i].Span.End > offset })
	if i < len(ix.Shells) && ix.Shells[i].Span.Start <= offset {
		return ix.Shells[i].Scope, true
	}
	return Global, false
}

func (ix *Index) Exported(name string, scope Scope) []int {
	var out []int
	for _, i := range ix.Lookup(name, NamespaceValues, scope) {
		if s := ix.Symbols[i]; s.Exported || ix.ExportAll {
			out = append(out, i)
		}
	}
	return out
}

func (ix *Index) Children(parent int) []int {
	var out []int
	for i, s := range ix.Symbols {
		if s.Parent == parent {
			out = append(out, i)
		}
	}
	return out
}
