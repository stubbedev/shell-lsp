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
		return name == overrides || name == LegacyBody
	case letSet:
	}
	return false
}

type entry struct {
	name         string
	binders      []syntax.Attr
	bindings     []syntax.Binding
	inherit      *syntax.Inherit
	value        syntax.Expr
	set          *setNode
	firstBinder  [1]syntax.Attr
	firstBinding [1]syntax.Binding
}

type extra struct {
	name      syntax.Attr
	inherited syntax.Attr
	value     syntax.Expr
	set       *setNode
}

const linearEntries = 8

type setNode struct {
	rec        bool
	owner      syntax.Node
	spans      []syntax.Span
	name       string
	parent     *setNode
	merged     *setNode
	entries    []*entry
	indexed    map[string]*entry
	froms      []*syntax.Paren
	statements []*syntax.Inherit
	extras     []extra
}

func (r *resolver) newSetNode(isRec bool) *setNode {
	if len(r.setChunk) == 0 {
		r.setChunk = make([]setNode, r.chunk)
	}
	n := &r.setChunk[0]
	r.setChunk = r.setChunk[1:]
	n.rec = isRec
	return n
}

func (n *setNode) lookup(name string) (*entry, bool) {
	if n.indexed != nil {
		e, ok := n.indexed[name]
		return e, ok
	}
	for _, e := range n.entries {
		if e.name == name {
			return e, true
		}
	}
	return nil, false
}

func (n *setNode) add(e *entry) {
	n.entries = append(n.entries, e)
	switch {
	case n.indexed != nil:
		n.indexed[e.name] = e
	case len(n.entries) > linearEntries:
		n.indexed = make(map[string]*entry, 2*len(n.entries))
		for _, known := range n.entries {
			n.indexed[known.name] = known
		}
	}
}

func (r *resolver) newEntry(name string, attr syntax.Attr, binding syntax.Binding) *entry {
	if len(r.entryChunk) == 0 {
		r.entryChunk = make([]entry, r.chunk)
	}
	e := &r.entryChunk[0]
	r.entryChunk = r.entryChunk[1:]
	e.name = name
	e.firstBinder[0] = attr
	e.firstBinding[0] = binding
	e.binders = e.firstBinder[:]
	e.bindings = e.firstBinding[:]
	return e
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
	n := r.newSetNode(set.Rec)
	n.owner = set
	if set.Rec {
		n.spans = []syntax.Span{set.Span()}
	}
	r.fill(n, set.Bindings)
	return n
}

func (r *resolver) fill(n *setNode, bindings []syntax.Binding) {
	n.entries = slices.Grow(n.entries, len(bindings))
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
		existing, exists := n.lookup(name)
		if !exists {
			fresh := r.newEntry(name, attr, stmt)
			fresh.inherit = stmt
			n.add(fresh)
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
		e, exists := cur.lookup(name)
		if last {
			r.terminal(cur, e, exists, attr, name, a)
			return
		}
		switch {
		case !exists:
			child := r.newSetNode(false)
			child.name, child.parent = name, cur
			fresh := r.newEntry(name, attr, a)
			fresh.set = child
			cur.add(fresh)
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
	child := r.newSetNode(false)
	cur.extras = append(cur.extras, extra{name: attr, set: child})
	return child
}

func (r *resolver) terminal(cur *setNode, e *entry, exists bool, attr syntax.Attr, name string, a *syntax.Assign) {
	set, isSet := syntax.Unparenthesized(a.Value).(*syntax.AttrSet)
	if !exists {
		fresh := r.newEntry(name, attr, a)
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
			e, exists := p.into.lookup(fe.name)
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
