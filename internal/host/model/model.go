package model

import (
	"cmp"
	"fmt"
	"hash/maphash"
	"iter"
	"slices"
	"sort"
	"strings"
	"sync"

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
	KindStage
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
	KindStage:     "stage",
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
	NamespaceStages
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
	case KindStage:
		return NamespaceStages
	}
	panic(fmt.Sprintf("model: kind %d has no namespace", k))
}

var nameSeed = maphash.MakeSeed()

type Scope int32

const Global Scope = 0

type Symbol struct {
	Name      string
	Span      Span
	Selection Span
	Parent    int
	Detail    string
	Doc       string
	Value     string
	Arity     Arity
	Scope     Scope
	Kind      Kind
	Elided    Elision
	Exported  bool
	Unused    bool
	Appends   bool
	Constant  bool
	Inert     bool
	Local     bool
	Passive   bool
	Listed    bool
	Hidden    bool
}

type Elision uint8

const (
	NotElided Elision = iota
	ElidedValue
	ElidedInherit
)

func (s Symbol) Description() string {
	switch s.Elided {
	case ElidedValue:
		return s.Detail + " = …;"
	case ElidedInherit:
		return "inherit " + s.Detail + ";"
	case NotElided:
	}
	return s.Detail
}

type Rewrite struct {
	Span   Span
	Before string
	After  string
	Remove Span
}

type Renames interface {
	Refusal(symbol int) string
	Conflict(symbol int, newName string) string
	SymbolRewrite(symbol int) *Rewrite
	RefRewrite(ref int) *Rewrite
}

const NoParent = -1

type Arity struct {
	Min, Max int32
}

var NoArity = Arity{Min: -1, Max: -1}

func (a Arity) Known() bool { return a.Min >= 0 }

func (a Arity) Accepts(n int) bool {
	return !a.Known() || n >= int(a.Min) && (a.Max == Variadic || n <= int(a.Max))
}

type RefCall struct {
	Qualifier []string
	ArgSpans  []Span
}

func Qualified(qualifier []string) *RefCall {
	if len(qualifier) == 0 {
		return nil
	}
	return &RefCall{Qualifier: qualifier}
}

type Ref struct {
	Name     string
	Span     Span
	Call     *RefCall
	Scope    Scope
	Args     int32
	Owner    int32
	Horizon  int32
	Kind     Kind
	Edge     bool
	Soft     bool
	Declares bool
	Base     bool
	Inert    bool
	Late     bool
}

const NoArgs = -1

func (r Ref) Qualifier() []string {
	if r.Call == nil {
		return nil
	}
	return r.Call.Qualifier
}

func (r Ref) ArgSpans() []Span {
	if r.Call == nil {
		return nil
	}
	return r.Call.ArgSpans
}

func (r *Ref) AddArg(span Span) {
	if r.Call == nil {
		r.Call = &RefCall{}
	}
	r.Call.ArgSpans = append(r.Call.ArgSpans, span)
}

func (r Ref) Qualified() bool { return len(r.Qualifier()) > 0 }

func (r Ref) FirstQualifier() string {
	if !r.Qualified() {
		return ""
	}
	return r.Call.Qualifier[0]
}

type Include struct {
	Span       Span
	Path       string
	Candidates []string
	Optional   bool
	Module     string
	Dynamic    bool
	Probe      bool
}

func (inc Include) Detached() Include {
	inc.Path = strings.Clone(inc.Path)
	inc.Module = strings.Clone(inc.Module)
	inc.Candidates = slices.Clone(inc.Candidates)
	for i, c := range inc.Candidates {
		inc.Candidates[i] = strings.Clone(c)
	}
	return inc
}

func MatchesPattern(pattern, name string) bool { return matches(pattern, name, 0) }

func MatchesRulePattern(pattern, name string) bool { return matches(pattern, name, 1) }

func matches(pattern, name string, stem int) bool {
	prefix, suffix, ok := strings.Cut(pattern, "%")
	if !ok {
		return pattern == name
	}
	return len(name) >= len(prefix)+len(suffix)+stem && strings.HasPrefix(name, prefix) && strings.HasSuffix(name, suffix)
}

type SearchPath struct {
	Pattern string
	Dirs    []string
}

type PathRef struct {
	Span Span
	Path string
}

type Shell struct {
	Span   Span
	Scope  Scope
	Phase  int
	Inputs []Input
}

type Input struct {
	Name string
	Span Span
	Head Span
	List string
}

type Hole struct {
	Span Span
	Head Span
	Exe  string
}

type Script struct {
	Name  string
	Head  string
	Fence string
}

type Fold struct {
	Span    Span
	Comment bool
}

type Runnable struct {
	Name   string
	Span   Span
	Params []RunParam
}

type RunParam struct {
	Name     string `json:"name"`
	Default  string `json:"default,omitempty"`
	Optional bool   `json:"optional"`
	Variadic bool   `json:"variadic"`
}

func (r Runnable) Required() int {
	n := 0
	for _, p := range r.Params {
		if !p.Optional {
			n++
		}
	}
	return n
}

func (r Runnable) Arity() Arity {
	a := Arity{Min: Narrow(r.Required()), Max: Narrow(len(r.Params))}
	if len(r.Params) > 0 && r.Params[len(r.Params)-1].Variadic {
		a.Max = Variadic
	}
	return a
}

func (r Runnable) Accepts(args int) bool { return r.Arity().Accepts(args) }

type Deferred struct {
	Symbol  int
	Span    Span
	Message string
	Used    lint.Code
	Unused  lint.Code
	Until   int
}

type Index struct {
	Symbols   []Symbol
	Deferred  []Deferred
	Refs      []Ref
	Includes  []Include
	Shells    []Shell
	Holes     []Hole
	Folds     []Fold
	Tokens    []semantic.Token
	Runnables []Runnable
	Findings  []lint.Finding
	ExportAll bool
	Broken    bool
	Opaque    bool
	Recipes   bool
	Inherits  map[Scope][]Scope
	Extends   map[Scope][]Scope
	Paths     []PathRef
	Phony     []string
	Fileless  []int
	Search    []SearchPath
	Branches  Alternatives
	Renames   Renames

	foldsOf  func() []Fold
	lookups  sync.Once
	byName   []int32
	nameRuns []int32
	byOwner  []int32
	byParent []int32
}

func (ix *Index) DeferFolds(compute func() []Fold) { ix.foldsOf = sync.OnceValue(compute) }

func (ix *Index) AllFolds() []Fold {
	if ix.foldsOf != nil {
		return ix.foldsOf()
	}
	return ix.Folds
}

type Branches []Span

func (b Branches) arm(offset int) int {
	return slices.IndexFunc(b, func(s Span) bool { return s.Start <= offset && offset < s.End })
}

type Alternatives []Branches

func (a Alternatives) Exclusive(x, y int) bool {
	return slices.ContainsFunc(a, func(b Branches) bool {
		armX, armY := b.arm(x), b.arm(y)
		return armX >= 0 && armY >= 0 && armX != armY
	})
}

func (ix *Index) tables() {
	ix.lookups.Do(func() {
		visible := 0
		for i := range ix.Symbols {
			if !ix.Symbols[i].Hidden {
				visible++
			}
		}
		ix.byName = make([]int32, len(ix.Symbols))
		ix.byParent = make([]int32, 0, visible)
		for i := range ix.Symbols {
			ix.byName[i] = Narrow(i)
			if !ix.Symbols[i].Hidden {
				ix.byParent = append(ix.byParent, Narrow(i))
			}
		}
		slices.SortFunc(ix.byName, func(a, b int32) int {
			x, y := &ix.Symbols[a], &ix.Symbols[b]
			return cmp.Or(cmp.Compare(x.Kind.Namespace(), y.Kind.Namespace()), strings.Compare(x.Name, y.Name), cmp.Compare(a, b))
		})
		slices.SortFunc(ix.byParent, func(a, b int32) int {
			return cmp.Or(cmp.Compare(ix.Symbols[a].Parent, ix.Symbols[b].Parent), cmp.Compare(a, b))
		})
		ix.buildNameRuns()
		edges := 0
		for i := range ix.Refs {
			if ix.Refs[i].Edge {
				edges++
			}
		}
		ix.byOwner = make([]int32, 0, edges)
		for i := range ix.Refs {
			if ix.Refs[i].Edge {
				ix.byOwner = append(ix.byOwner, Narrow(i))
			}
		}
		slices.SortFunc(ix.byOwner, func(a, b int32) int {
			return cmp.Or(cmp.Compare(ix.Refs[a].Owner, ix.Refs[b].Owner), cmp.Compare(a, b))
		})
	})
}

func equalRun(sorted []int32, compare func(item int32) int) []int32 {
	first := sort.Search(len(sorted), func(i int) bool { return compare(sorted[i]) >= 0 })
	last := first
	for last < len(sorted) && compare(sorted[last]) == 0 {
		last++
	}
	return sorted[first:last]
}

func nameHash(ns Namespace, name string) uint64 {
	return maphash.String(nameSeed, name) + uint64(ns)
}

func (ix *Index) sameName(item int32, ns Namespace, name string) bool {
	s := &ix.Symbols[item]
	return s.Kind.Namespace() == ns && s.Name == name
}

func (ix *Index) buildNameRuns() {
	size := 1
	for size < 2*len(ix.byName) {
		size *= 2
	}
	ix.nameRuns = make([]int32, size)
	mask := uint64(size - 1) //nolint:gosec // size is a positive power of two
	for at, item := range ix.byName {
		s := &ix.Symbols[item]
		ns := s.Kind.Namespace()
		if at > 0 && ix.sameName(ix.byName[at-1], ns, s.Name) {
			continue
		}
		slot := nameHash(ns, s.Name) & mask
		for ix.nameRuns[slot] != 0 {
			slot = (slot + 1) & mask
		}
		ix.nameRuns[slot] = Narrow(at) + 1
	}
}

func (ix *Index) named(ns Namespace, name string) []int32 {
	ix.tables()
	if len(ix.byName) == 0 {
		return nil
	}
	mask := uint64(len(ix.nameRuns) - 1) //nolint:gosec // the table is never empty here
	for slot := nameHash(ns, name) & mask; ix.nameRuns[slot] != 0; slot = (slot + 1) & mask {
		first := int(ix.nameRuns[slot]) - 1
		if !ix.sameName(ix.byName[first], ns, name) {
			continue
		}
		last := first + 1
		for last < len(ix.byName) && ix.sameName(ix.byName[last], ns, name) {
			last++
		}
		return ix.byName[first:last]
	}
	return nil
}

func (ix *Index) Binds(scope Scope, name string) bool {
	for _, ns := range linkedNamespaces {
		for _, i := range ix.named(ns, name) {
			if ix.Symbols[i].Scope == scope {
				return true
			}
		}
	}
	return false
}

func (ix *Index) edgesOf(owner int) []int32 {
	ix.tables()
	return equalRun(ix.byOwner, func(item int32) int { return cmp.Compare(int(ix.Refs[item].Owner), owner) })
}

type Certainty uint8

const (
	WhenComplete Certainty = iota
	Regardless
)

type Expansion uint8

const (
	Eager Expansion = iota
	OnUse
)

type Rules struct {
	Unknown     lint.Code
	UnknownWhen Certainty
	Arity       lint.Code
	Circular    lint.Code
	Expanded    Expansion
	Unused      lint.Code
}

type Checks struct {
	Of         func(Kind) Rules
	Deprecated lint.Code
	Missing    lint.Code
	Absent     lint.Code
	NotPhony   lint.Code
}

func (c Checks) Active() bool {
	if c.Deprecated != 0 || c.Missing != 0 || c.Absent != 0 || c.NotPhony != 0 {
		return true
	}
	for kind := range Kinds() {
		if c.Of(kind) != (Rules{}) {
			return true
		}
	}
	return false
}

func Nested(offset int, spans []Span) []Span {
	var inside []Span
	for _, s := range spans {
		if s.Start <= offset && offset <= s.End && s.End > s.Start {
			inside = append(inside, s)
		}
	}
	slices.SortStableFunc(inside, func(a, b Span) int { return (a.End - a.Start) - (b.End - b.Start) })
	var out []Span
	for _, s := range inside {
		if n := len(out); n > 0 && (s == out[n-1] || s.Start > out[n-1].Start || s.End < out[n-1].End) {
			continue
		}
		out = append(out, s)
	}
	return out
}

type Expect struct {
	Kinds []Kind
	Span  Span
	Scope Scope
}

type Invocation struct {
	Name      string
	Qualifier []string
	Kind      Kind
	Active    int
	Scope     Scope
}

type Builtin struct {
	Name       string
	Kind       Kind
	Signature  string
	Params     []string
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
		if !s.Passive && s.Selection.Touches(offset) && s.Selection.End > s.Selection.Start {
			return Hit{Name: s.Name, Kind: s.Kind, Span: s.Selection, Scope: s.Scope, Symbol: i, Declaration: true}, true
		}
	}
	for _, r := range ix.Refs {
		if r.Span.Touches(offset) && r.Span.End > r.Span.Start {
			return Hit{Name: r.Name, Kind: r.Kind, Span: r.Span, Scope: r.Scope, Qualifier: r.Qualifier(), Symbol: NoSymbol}, true
		}
	}
	return Hit{}, false
}

func (ix *Index) DeclarationAt(offset int) (Hit, bool) {
	for i, s := range ix.Symbols {
		if !s.Hidden && s.Selection.Touches(offset) && s.Selection.End > s.Selection.Start {
			return Hit{Name: s.Name, Kind: s.Kind, Span: s.Selection, Scope: s.Scope, Symbol: i, Declaration: true}, true
		}
	}
	return Hit{}, false
}

func (ix *Index) Lookup(name string, ns Namespace, scope Scope) []int {
	var local, global []int
	for _, i := range ix.named(ns, name) {
		s := ix.Symbols[i]
		switch s.Scope {
		case scope:
			if scope != Global {
				local = append(local, int(i))
				continue
			}
			global = append(global, int(i))
		case Global:
			global = append(global, int(i))
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

func (ix *Index) Children(parent int) []int32 {
	ix.tables()
	return equalRun(ix.byParent, func(item int32) int { return cmp.Compare(ix.Symbols[item].Parent, parent) })
}

func Narrow(n int) int32 {
	return int32(n) //nolint:gosec // offsets, counts and indexes of a source file, which the transport caps well below 2^31
}
