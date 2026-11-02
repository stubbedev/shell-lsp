package scope

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

type setKind uint8

const (
	letSet setKind = iota
	recSet
	legacySet
)

func (k setKind) scope() ScopeKind {
	if k == recSet {
		return RecScope
	}
	return LetScope
}

func (k setKind) defKind(e *entry) DefKind {
	switch {
	case e.inherit != nil:
		return InheritDef
	case k == recSet:
		return RecDef
	}
	return LetDef
}

func (k setKind) readsItself(name string) bool {
	switch k {
	case recSet:
		return name == overrides
	case legacySet:
		return name == overrides || name == legacyBody
	case letSet:
	}
	return false
}

type entry struct {
	name     string
	binders  []syntax.Attr
	bindings []syntax.Binding
	inherit  *syntax.Inherit
	value    syntax.Expr
	set      *setNode
}

type extra struct {
	name      syntax.Attr
	inherited syntax.Attr
	value     syntax.Expr
	set       *setNode
}

type setNode struct {
	rec        bool
	owner      syntax.Node
	spans      []syntax.Span
	name       string
	parent     *setNode
	merged     *setNode
	entries    []*entry
	index      map[string]*entry
	froms      []*syntax.Paren
	statements []*syntax.Inherit
	extras     []extra
}

func newSetNode(rec bool) *setNode {
	return &setNode{rec: rec, index: map[string]*entry{}}
}

func (n *setNode) add(e *entry) {
	n.index[e.name] = e
	n.entries = append(n.entries, e)
}

func (n *setNode) path(leaf string) string {
	names := []string{leaf}
	for at := n; ; at = at.parent {
		for at.merged != nil {
			at = at.merged
		}
		if at.parent == nil {
			break
		}
		names = append(names, at.name)
	}
	slices.Reverse(names)
	return strings.Join(names, ".")
}

type pendingDuplicate struct {
	attr syntax.Attr
	node *setNode
	name string
}

func (d pendingDuplicate) path() string { return d.node.path(d.name) }

func (r *resolver) duplicate(n *setNode, attr syntax.Attr, name string) {
	r.duplicates = append(r.duplicates, pendingDuplicate{attr: attr, node: n, name: name})
}

func (r *resolver) literal(set *syntax.AttrSet) *setNode {
	n := newSetNode(set.Rec)
	n.owner = set
	if set.Rec {
		n.spans = []syntax.Span{set.Span()}
	}
	r.fill(n, set.Bindings)
	return n
}

func (r *resolver) fill(n *setNode, bindings []syntax.Binding) {
	for _, b := range bindings {
		switch b := b.(type) {
		case *syntax.Assign:
			r.assign(n, b)
		case *syntax.Inherit:
			r.inheritStatement(n, b)
		}
	}
}

func (r *resolver) inheritStatement(n *setNode, stmt *syntax.Inherit) {
	if stmt.From != nil {
		n.froms = append(n.froms, stmt.From)
	}
	n.statements = append(n.statements, stmt)
	for _, attr := range stmt.Attrs {
		name, ok := syntax.AttrName(attr)
		if !ok {
			n.extras = append(n.extras, extra{name: attr})
			continue
		}
		existing, exists := n.index[name]
		if !exists {
			n.add(&entry{name: name, binders: []syntax.Attr{attr}, bindings: []syntax.Binding{stmt}, inherit: stmt})
			continue
		}
		r.duplicate(n, attr, name)
		existing.binders = append(existing.binders, attr)
		existing.bindings = append(existing.bindings, stmt)
		if stmt.From == nil {
			n.extras = append(n.extras, extra{inherited: attr})
		}
	}
}

func (r *resolver) assign(n *setNode, a *syntax.Assign) {
	cur := n
	for i, attr := range a.Path {
		last := i == len(a.Path)-1
		name, ok := syntax.AttrName(attr)
		if !ok {
			if cur = r.dynamicStep(cur, attr, a, last); cur == nil {
				return
			}
			continue
		}
		e, exists := cur.index[name]
		if last {
			r.terminal(cur, e, exists, attr, name, a)
			return
		}
		switch {
		case !exists:
			child := newSetNode(false)
			child.name, child.parent = name, cur
			cur.add(&entry{name: name, binders: []syntax.Attr{attr}, bindings: []syntax.Binding{a}, set: child})
			cur = child
		case e.set == nil:
			r.duplicate(cur, attr, name)
			e.binders = append(e.binders, attr)
			e.bindings = append(e.bindings, a)
			cur.extras = append(cur.extras, extra{value: a.Value})
			return
		default:
			e.binders = append(e.binders, attr)
			e.bindings = append(e.bindings, a)
			cur = e.set
		}
		if cur.rec {
			cur.spans = append(cur.spans, a.Span())
		}
	}
}

func (r *resolver) dynamicStep(cur *setNode, attr syntax.Attr, a *syntax.Assign, last bool) *setNode {
	if last {
		cur.extras = append(cur.extras, extra{name: attr, value: a.Value})
		return nil
	}
	child := newSetNode(false)
	cur.extras = append(cur.extras, extra{name: attr, set: child})
	return child
}

func (r *resolver) terminal(cur *setNode, e *entry, exists bool, attr syntax.Attr, name string, a *syntax.Assign) {
	set, isSet := syntax.Unparenthesized(a.Value).(*syntax.AttrSet)
	if !exists {
		fresh := &entry{name: name, binders: []syntax.Attr{attr}, bindings: []syntax.Binding{a}}
		if isSet {
			fresh.set = r.literal(set)
			fresh.set.name, fresh.set.parent = name, cur
		} else {
			fresh.value = a.Value
		}
		cur.add(fresh)
		return
	}
	e.binders = append(e.binders, attr)
	e.bindings = append(e.bindings, a)
	if isSet && e.set != nil {
		r.merge(e.set, r.literal(set))
		return
	}
	r.duplicate(cur, attr, name)
	if isSet {
		cur.extras = append(cur.extras, extra{set: r.literal(set)})
		return
	}
	cur.extras = append(cur.extras, extra{value: a.Value})
}

type mergePair struct{ into, from *setNode }

func (r *resolver) merge(into, from *setNode) {
	pending := []mergePair{{into, from}}
	for len(pending) > 0 {
		p := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		p.from.merged = p.into
		if p.into.rec {
			p.into.spans = append(p.into.spans, p.from.literalSpans()...)
		}
		p.into.froms = append(p.into.froms, p.from.froms...)
		p.into.statements = append(p.into.statements, p.from.statements...)
		p.into.extras = append(p.into.extras, p.from.extras...)
		for _, fe := range p.from.entries {
			e, exists := p.into.index[fe.name]
			switch {
			case !exists:
				p.into.add(fe)
				if fe.set != nil {
					fe.set.parent = p.into
				}
			case e.set != nil && fe.set != nil:
				e.binders = append(e.binders, fe.binders...)
				e.bindings = append(e.bindings, fe.bindings...)
				pending = append(pending, mergePair{e.set, fe.set})
			default:
				r.duplicate(p.into, fe.binders[0], fe.name)
				e.binders = append(e.binders, fe.binders...)
				e.bindings = append(e.bindings, fe.bindings...)
				p.into.extras = append(p.into.extras, fe.orphaned()...)
			}
		}
	}
}

func (n *setNode) literalSpans() []syntax.Span {
	if n.owner == nil {
		return nil
	}
	return []syntax.Span{n.owner.Span()}
}

func (e *entry) orphaned() []extra {
	switch {
	case e.set != nil:
		return []extra{{set: e.set}}
	case e.value != nil:
		return []extra{{value: e.value}}
	case e.inherit.From == nil:
		return []extra{{inherited: e.binders[0]}}
	}
	return nil
}
