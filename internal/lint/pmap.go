package lint

import "math/bits"

const (
	trieBits     = 4
	trieMask     = 1<<trieBits - 1
	trieMaxDepth = 64 / trieBits
	fnvOffset    = 14695981039346656037
	fnvPrime     = 1099511628211
)

type pmap[V comparable] struct {
	root *pnode[V]
}

type pnode[V comparable] struct {
	entryMap uint32
	childMap uint32
	entries  []pentry[V]
	children []*pnode[V]
}

type pentry[V comparable] struct {
	key   string
	value V
}

func hashKey(key string) uint64 {
	h := uint64(fnvOffset)
	for i := range len(key) {
		h ^= uint64(key[i])
		h *= fnvPrime
	}
	return h
}

func slotBit(h uint64, depth int) uint32 {
	return 1 << (uint32(h>>(uint(depth)*trieBits)) & trieMask) //nolint:gosec
}

func index(bitmap, bit uint32) int {
	return bits.OnesCount32(bitmap & (bit - 1))
}

func (m pmap[V]) get(key string) (V, bool) {
	h := hashKey(key)
	n := m.root
	for depth := 0; n != nil; depth++ {
		if depth == trieMaxDepth {
			return collisionGet(n.entries, key)
		}
		bit := slotBit(h, depth)
		switch {
		case n.entryMap&bit != 0:
			if e := n.entries[index(n.entryMap, bit)]; e.key == key {
				return e.value, true
			}
			n = nil
		case n.childMap&bit != 0:
			n = n.children[index(n.childMap, bit)]
		default:
			n = nil
		}
	}
	var zero V
	return zero, false
}

func collisionGet[V comparable](entries []pentry[V], key string) (V, bool) {
	for _, e := range entries {
		if e.key == key {
			return e.value, true
		}
	}
	var zero V
	return zero, false
}

func (m pmap[V]) set(key string, value V) pmap[V] {
	return pmap[V]{root: insert(m.root, pentry[V]{key: key, value: value}, hashKey(key), 0)}
}

func insert[V comparable](n *pnode[V], e pentry[V], h uint64, depth int) *pnode[V] {
	if n == nil {
		if depth == trieMaxDepth {
			return &pnode[V]{entries: []pentry[V]{e}}
		}
		return &pnode[V]{entryMap: slotBit(h, depth), entries: []pentry[V]{e}}
	}
	if depth == trieMaxDepth {
		return collisionInsert(n, e)
	}
	bit := slotBit(h, depth)
	switch {
	case n.entryMap&bit != 0:
		i := index(n.entryMap, bit)
		old := n.entries[i]
		if old.key == e.key {
			if old.value == e.value {
				return n
			}
			entries := append([]pentry[V](nil), n.entries...)
			entries[i] = e
			return &pnode[V]{entryMap: n.entryMap, childMap: n.childMap, entries: entries, children: n.children}
		}
		child := insert(insert(nil, old, hashKey(old.key), depth+1), e, h, depth+1)
		return &pnode[V]{
			entryMap: n.entryMap &^ bit,
			childMap: n.childMap | bit,
			entries:  without(n.entries, i),
			children: inserted(n.children, index(n.childMap, bit), child),
		}
	case n.childMap&bit != 0:
		i := index(n.childMap, bit)
		child := insert(n.children[i], e, h, depth+1)
		if child == n.children[i] {
			return n
		}
		return &pnode[V]{entryMap: n.entryMap, childMap: n.childMap, entries: n.entries, children: replaced(n.children, i, child)}
	}
	return &pnode[V]{
		entryMap: n.entryMap | bit,
		childMap: n.childMap,
		entries:  inserted(n.entries, index(n.entryMap, bit), e),
		children: n.children,
	}
}

func collisionInsert[V comparable](n *pnode[V], e pentry[V]) *pnode[V] {
	for i, old := range n.entries {
		if old.key != e.key {
			continue
		}
		if old.value == e.value {
			return n
		}
		return &pnode[V]{entries: replaced(n.entries, i, e)}
	}
	return &pnode[V]{entries: inserted(n.entries, len(n.entries), e)}
}

func (m pmap[V]) remove(key string) pmap[V] {
	return pmap[V]{root: erase(m.root, hashKey(key), key, 0)}
}

func erase[V comparable](n *pnode[V], h uint64, key string, depth int) *pnode[V] {
	if n == nil {
		return nil
	}
	if depth == trieMaxDepth {
		for i, e := range n.entries {
			if e.key == key {
				return nonEmpty(&pnode[V]{entries: without(n.entries, i)})
			}
		}
		return n
	}
	bit := slotBit(h, depth)
	switch {
	case n.entryMap&bit != 0:
		i := index(n.entryMap, bit)
		if n.entries[i].key != key {
			return n
		}
		return nonEmpty(&pnode[V]{entryMap: n.entryMap &^ bit, childMap: n.childMap, entries: without(n.entries, i), children: n.children})
	case n.childMap&bit != 0:
		i := index(n.childMap, bit)
		child := erase(n.children[i], h, key, depth+1)
		switch {
		case child == n.children[i]:
			return n
		case child == nil:
			return nonEmpty(&pnode[V]{entryMap: n.entryMap, childMap: n.childMap &^ bit, entries: n.entries, children: without(n.children, i)})
		case child.childMap == 0 && len(child.entries) == 1:
			return &pnode[V]{
				entryMap: n.entryMap | bit,
				childMap: n.childMap &^ bit,
				entries:  inserted(n.entries, index(n.entryMap, bit), child.entries[0]),
				children: without(n.children, i),
			}
		}
		return &pnode[V]{entryMap: n.entryMap, childMap: n.childMap, entries: n.entries, children: replaced(n.children, i, child)}
	}
	return n
}

func nonEmpty[V comparable](n *pnode[V]) *pnode[V] {
	if len(n.entries) == 0 && len(n.children) == 0 {
		return nil
	}
	return n
}

func inserted[T any](s []T, i int, v T) []T {
	out := make([]T, len(s)+1)
	copy(out, s[:i])
	out[i] = v
	copy(out[i+1:], s[i:])
	return out
}

func replaced[T any](s []T, i int, v T) []T {
	out := append([]T(nil), s...)
	out[i] = v
	return out
}

func without[T any](s []T, i int) []T {
	if len(s) == 1 {
		return nil
	}
	out := make([]T, 0, len(s)-1)
	out = append(out, s[:i]...)
	return append(out, s[i+1:]...)
}

func (m pmap[V]) each(f func(key string, value V)) {
	m.root.each(f)
}

func (n *pnode[V]) each(f func(key string, value V)) {
	if n == nil {
		return
	}
	for _, e := range n.entries {
		f(e.key, e.value)
	}
	for _, c := range n.children {
		c.each(f)
	}
}

func (n *pnode[V]) eachKey(f func(key string)) {
	n.each(func(key string, _ V) { f(key) })
}

func (m pmap[V]) diff(o pmap[V], f func(key string)) {
	diffNodes(m.root, o.root, 0, f)
}

func diffNodes[V comparable](a, b *pnode[V], depth int, f func(key string)) {
	switch {
	case a == b:
		return
	case a == nil:
		b.eachKey(f)
		return
	case b == nil:
		a.eachKey(f)
		return
	case depth == trieMaxDepth:
		diffCollisions(a.entries, b.entries, f)
		return
	}
	for union := a.entryMap | a.childMap | b.entryMap | b.childMap; union != 0; union &= union - 1 {
		bit := union & -union
		aEntry, aChild := a.entryMap&bit != 0, a.childMap&bit != 0
		bEntry, bChild := b.entryMap&bit != 0, b.childMap&bit != 0
		switch {
		case aChild && bChild:
			diffNodes(a.children[index(a.childMap, bit)], b.children[index(b.childMap, bit)], depth+1, f)
		case aEntry && bEntry:
			ea, eb := a.entries[index(a.entryMap, bit)], b.entries[index(b.entryMap, bit)]
			if ea.key != eb.key {
				f(ea.key)
				f(eb.key)
			} else if ea.value != eb.value {
				f(ea.key)
			}
		case aEntry && bChild:
			diffEntryNode(a.entries[index(a.entryMap, bit)], b.children[index(b.childMap, bit)], f)
		case aChild && bEntry:
			diffEntryNode(b.entries[index(b.entryMap, bit)], a.children[index(a.childMap, bit)], f)
		case aEntry:
			f(a.entries[index(a.entryMap, bit)].key)
		case aChild:
			a.children[index(a.childMap, bit)].eachKey(f)
		case bEntry:
			f(b.entries[index(b.entryMap, bit)].key)
		default:
			b.children[index(b.childMap, bit)].eachKey(f)
		}
	}
}

func diffEntryNode[V comparable](e pentry[V], n *pnode[V], f func(key string)) {
	found := false
	n.each(func(key string, value V) {
		if key != e.key {
			f(key)
			return
		}
		found = true
		if value != e.value {
			f(key)
		}
	})
	if !found {
		f(e.key)
	}
}

func diffCollisions[V comparable](a, b []pentry[V], f func(key string)) {
	for _, ea := range a {
		if vb, ok := collisionGet(b, ea.key); !ok || vb != ea.value {
			f(ea.key)
		}
	}
	for _, eb := range b {
		if _, ok := collisionGet(a, eb.key); !ok {
			f(eb.key)
		}
	}
}
