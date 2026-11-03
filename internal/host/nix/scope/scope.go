package scope

import (
	"cmp"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/nix/builtins"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

const (
	maxPresizedNames = 1 << 16
	CurPos           = "__curPos"
	overrides        = "__overrides"
	LegacyBody       = "body"
)

var evaluatorReads = []string{"__sub", "__mul", "__div", "__lessThan", "__findFile", "__nixPath"}

type DefKind uint8

const (
	LetDef DefKind = iota
	RecDef
	ArgDef
	FormalDef
	AliasDef
	InheritDef
)

type Def struct {
	Name     string
	Binders  []syntax.Node
	Bindings []syntax.Binding
	Scope    *Scope
	uses     []Use
	ID       int32
	Kind     DefKind
	Implicit bool
}

func (d *Def) Uses() []Use { return d.uses }

func (d *Def) Used() bool { return d.Implicit || len(d.uses) > 0 }

type RefKind uint8

const (
	Undefined RefKind = iota
	Definition
	Global
	Dynamic
)

type Ref struct {
	Kind    RefKind
	Def     *Def
	Builtin *builtins.Builtin
	With    *syntax.With
}

type Use struct {
	Node    syntax.Node
	Name    string
	Ref     Ref
	Inherit bool
}

type ScopeKind uint8

const (
	FileScope ScopeKind = iota
	LetScope
	RecScope
	LambdaScope
	WithScope
	InheritScope
)

type Scope struct {
	Kind    ScopeKind
	Parent  *Scope
	Defs    []*Def
	With    *syntax.With
	regions []region
}

type region struct {
	span  syntax.Span
	scope *Scope
}

type Duplicate struct {
	Attr syntax.Attr
	Path string
}

type slotRef uint32

const (
	refKindBits = 2
	refKnown    = slotRef(1) << refKindBits
	refIndexAt  = refKindBits + 1
)

func packRef(kind RefKind, index int) slotRef {
	return slotRef(index)<<refIndexAt | refKnown | slotRef(kind) //nolint:gosec // an index counts syntax nodes of one file, which the transport caps well below 2^29
}

func (s slotRef) kind() RefKind { return RefKind(s & (refKnown - 1)) }

func (s slotRef) index() int { return int(s >> refIndexAt) }

func packDef(d *Def) int32 { return d.ID + 1 }

type Table struct {
	refs       []slotRef
	binders    []int32
	withs      []*syntax.With
	owners     map[syntax.Node]*Scope
	outerWiths map[*syntax.With]*syntax.With
	root       *Scope
	scopes     []*Scope
	uses       []Use
	defs       []*Def
	duplicates []Duplicate
	supplied   map[*Def]syntax.Expr
}

func (t *Table) Of(id *syntax.Ident) (Ref, bool) {
	return t.resolved(id.Slot())
}

func (t *Table) resolved(slot int) (Ref, bool) {
	packed := t.refs[slot]
	if packed&refKnown == 0 {
		return Ref{}, false
	}
	switch packed.kind() {
	case Definition:
		return Ref{Kind: Definition, Def: t.defs[packed.index()]}, true
	case Global:
		return Ref{Kind: Global, Builtin: builtins.At(packed.index())}, true
	case Dynamic:
		return Ref{Kind: Dynamic, With: t.withs[packed.index()]}, true
	case Undefined:
	}
	return Ref{Kind: Undefined}, true
}

func (t *Table) Inherited(attr syntax.Attr) (Ref, bool) {
	return t.resolved(attr.Slot())
}

func (t *Table) Binder(node syntax.Node) (*Def, bool) {
	var packed int32
	switch node := node.(type) {
	case *syntax.Ident:
		packed = t.binders[node.Slot()]
	case syntax.Attr:
		packed = t.binders[node.Slot()]
	}
	if packed == 0 {
		return nil, false
	}
	return t.defs[packed-1], true
}

func (t *Table) Supplied(d *Def) (syntax.Expr, bool) {
	value, ok := t.supplied[d]
	return value, ok
}

func (t *Table) supply(binder *syntax.Ident, value syntax.Expr) {
	d, bound := t.Binder(binder)
	if !bound {
		return
	}
	if t.supplied == nil {
		t.supplied = map[*Def]syntax.Expr{}
	}
	t.supplied[d] = value
}

func (t *Table) Uses() []Use { return t.uses }

func (t *Table) ForgetUses() {
	t.uses = nil
	for _, d := range t.defs {
		d.uses = nil
	}
}

func (t *Table) Defs() []*Def { return t.defs }

func (t *Table) Duplicates() []Duplicate { return t.duplicates }

func (t *Table) ScopeOf(owner syntax.Node) (*Scope, bool) {
	s, ok := t.owners[owner]
	return s, ok
}

func (t *Table) Withs(innermost *syntax.With) []*syntax.With {
	var out []*syntax.With
	for w := innermost; w != nil; w = t.outerWiths[w] {
		out = append(out, w)
	}
	return out
}

func (t *Table) ScopeAt(offset int) *Scope {
	s := t.root
	for {
		after, _ := slices.BinarySearchFunc(s.regions, offset, func(r region, at int) int {
			if r.span.Start <= at {
				return -1
			}
			return 1
		})
		if after == 0 || offset > s.regions[after-1].span.End {
			return s
		}
		s = s.regions[after-1].scope
	}
}

type Name struct {
	Name string
	Ref  Ref
}

type Names struct {
	Visible []Name
	Withs   []*syntax.With
}

func (t *Table) NamesAt(offset int) Names {
	var out Names
	seen := map[string]bool{CurPos: true}
	for s := t.ScopeAt(offset); s != nil; s = s.Parent {
		if s.With != nil {
			out.Withs = append(out.Withs, s.With)
		}
		for _, d := range s.Defs {
			if !seen[d.Name] {
				seen[d.Name] = true
				out.Visible = append(out.Visible, Name{Name: d.Name, Ref: Ref{Kind: Definition, Def: d}})
			}
		}
	}
	for _, b := range builtins.All() {
		if b.Global && (b.Name == CurPos || !seen[b.Name]) {
			out.Visible = append(out.Visible, Name{Name: b.Name, Ref: Ref{Kind: Global, Builtin: &b}})
		}
	}
	slices.SortFunc(out.Visible, func(a, b Name) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func Resolve(f *syntax.File) *Table {
	root := &Scope{Kind: FileScope}
	r := &resolver{
		table: &Table{
			refs:       make([]slotRef, f.Slots),
			binders:    make([]int32, f.Slots),
			owners:     map[syntax.Node]*Scope{},
			outerWiths: map[*syntax.With]*syntax.With{},
			root:       root,
			scopes:     []*Scope{root},
			uses:       make([]Use, 0, f.Reads),
			defs:       make([]*Def, 0, f.Binders),
		},
		shadow: make(map[string]*Def, min(f.Binders, maxPresizedNames)),
		scope:  root,
	}
	r.expr(f.Root)
	for _, e := range f.Stray {
		r.expr(e)
	}
	r.finish()
	return r.table
}

func (t *Table) groupUses() {
	counts := make([]int32, len(t.defs))
	total := 0
	for i := range t.uses {
		if d := t.uses[i].Ref.Def; d != nil {
			counts[d.ID]++
			total++
		}
	}
	grouped := make([]Use, total)
	for _, d := range t.defs {
		n := counts[d.ID]
		d.uses = grouped[:0:n]
		grouped = grouped[n:]
	}
	for i := range t.uses {
		if d := t.uses[i].Ref.Def; d != nil {
			d.uses = append(d.uses, t.uses[i])
		}
	}
}

func (t *Table) renumberDefs() {
	placed := make([]int32, len(t.defs))
	for at, d := range t.defs {
		placed[d.ID] = int32(at)
		d.ID = int32(at)
	}
	for slot, packed := range t.refs {
		if packed&refKnown != 0 && packed.kind() == Definition {
			t.refs[slot] = packRef(Definition, int(placed[packed.index()]))
		}
	}
	for slot, packed := range t.binders {
		if packed != 0 {
			t.binders[slot] = placed[packed-1] + 1
		}
	}
}

func (r *resolver) finish() {
	t := r.table
	byStart := func(a, b syntax.Node) int { return cmp.Compare(a.Span().Start, b.Span().Start) }
	slices.SortStableFunc(t.uses, func(a, b Use) int { return byStart(a.Node, b.Node) })
	t.groupUses()
	slices.SortStableFunc(t.defs, func(a, b *Def) int { return byStart(a.Binders[0], b.Binders[0]) })
	t.renumberDefs()
	for _, s := range t.scopes {
		slices.SortStableFunc(s.regions, func(a, b region) int { return cmp.Compare(a.span.Start, b.span.Start) })
	}
	t.duplicates = make([]Duplicate, len(r.duplicates))
	for i, d := range r.duplicates {
		t.duplicates[i] = Duplicate{Attr: d.attr, Path: d.path()}
	}
	slices.SortStableFunc(t.duplicates, func(a, b Duplicate) int { return byStart(a.Attr, b.Attr) })
}
