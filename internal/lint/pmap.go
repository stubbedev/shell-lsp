package lint

import "math/bits"

const (
	trieBits     = 5
	trieWidth    = 1 << trieBits
	trieMask     = trieWidth - 1
	trieMaxDepth = 64 / trieBits
	fnvOffset    = 14695981039346656037
	fnvPrime     = 1099511628211
)

type pmap[V comparable] struct {
	root *pnode[V]
	size int
}

type pnode[V comparable] struct {
	bitmap   uint32
	slots    []pslot[V]
	overflow []pentry[V]
}

type pslot[V comparable] struct {
	child *pnode[V]
	entry pentry[V]
}

type pentry[V comparable] struct {
	hash  uint64
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

func slotIndex(h uint64, depth int) uint32 {
	return uint32(h>>(uint(depth)*trieBits)) & trieMask //nolint:gosec
}

func (n *pnode[V]) position(bit uint32) int {
	return bits.OnesCount32(n.bitmap & (bit - 1))
}

func (m pmap[V]) get(key string) (V, bool) {
	h := hashKey(key)
	n := m.root
	for depth := 0; n != nil; depth++ {
		if depth == trieMaxDepth {
			for _, e := range n.overflow {
				if e.key == key {
					return e.value, true
				}
			}
			break
		}
		bit := uint32(1) << slotIndex(h, depth)
		if n.bitmap&bit == 0 {
			break
		}
		slot := n.slots[n.position(bit)]
		if slot.child == nil {
			if slot.entry.key == key {
				return slot.entry.value, true
			}
			break
		}
		n = slot.child
	}
	var zero V
	return zero, false
}

func (m pmap[V]) set(key string, value V) pmap[V] {
	root, added := insert(m.root, pentry[V]{hash: hashKey(key), key: key, value: value}, 0)
	if added {
		return pmap[V]{root: root, size: m.size + 1}
	}
	return pmap[V]{root: root, size: m.size}
}

func insert[V comparable](n *pnode[V], e pentry[V], depth int) (*pnode[V], bool) {
	if n == nil {
		n = &pnode[V]{}
	}
	if depth == trieMaxDepth {
		for i, old := range n.overflow {
			if old.key == e.key {
				if old.value == e.value {
					return n, false
				}
				out := &pnode[V]{overflow: append([]pentry[V](nil), n.overflow...)}
				out.overflow[i] = e
				return out, false
			}
		}
		return &pnode[V]{overflow: append(append([]pentry[V](nil), n.overflow...), e)}, true
	}
	bit := uint32(1) << slotIndex(e.hash, depth)
	pos := n.position(bit)
	if n.bitmap&bit == 0 {
		slots := make([]pslot[V], len(n.slots)+1)
		copy(slots, n.slots[:pos])
		slots[pos] = pslot[V]{entry: e}
		copy(slots[pos+1:], n.slots[pos:])
		return &pnode[V]{bitmap: n.bitmap | bit, slots: slots}, true
	}
	slot := n.slots[pos]
	var replacement pslot[V]
	added := false
	switch {
	case slot.child != nil:
		child, grew := insert(slot.child, e, depth+1)
		if child == slot.child {
			return n, false
		}
		replacement, added = pslot[V]{child: child}, grew
	case slot.entry.key == e.key:
		if slot.entry.value == e.value {
			return n, false
		}
		replacement = pslot[V]{entry: e}
	default:
		child, _ := insert(nil, slot.entry, depth+1)
		child, _ = insert(child, e, depth+1)
		replacement, added = pslot[V]{child: child}, true
	}
	slots := append([]pslot[V](nil), n.slots...)
	slots[pos] = replacement
	return &pnode[V]{bitmap: n.bitmap, slots: slots}, added
}

func (m pmap[V]) remove(key string) pmap[V] {
	root, removed := erase(m.root, hashKey(key), key, 0)
	if !removed {
		return m
	}
	return pmap[V]{root: root, size: m.size - 1}
}

func erase[V comparable](n *pnode[V], h uint64, key string, depth int) (*pnode[V], bool) {
	if n == nil {
		return nil, false
	}
	if depth == trieMaxDepth {
		for i, e := range n.overflow {
			if e.key == key {
				rest := append(append([]pentry[V](nil), n.overflow[:i]...), n.overflow[i+1:]...)
				if len(rest) == 0 {
					return nil, true
				}
				return &pnode[V]{overflow: rest}, true
			}
		}
		return n, false
	}
	bit := uint32(1) << slotIndex(h, depth)
	if n.bitmap&bit == 0 {
		return n, false
	}
	pos := n.position(bit)
	slot := n.slots[pos]
	if slot.child == nil {
		if slot.entry.key != key {
			return n, false
		}
		return n.without(pos, bit), true
	}
	child, removed := erase(slot.child, h, key, depth+1)
	if !removed {
		return n, false
	}
	slots := append([]pslot[V](nil), n.slots...)
	switch {
	case child == nil:
		return n.without(pos, bit), true
	case len(child.slots) == 1 && child.slots[0].child == nil && len(child.overflow) == 0:
		slots[pos] = pslot[V]{entry: child.slots[0].entry}
	case len(child.overflow) == 1 && len(child.slots) == 0:
		slots[pos] = pslot[V]{entry: child.overflow[0]}
	default:
		slots[pos] = pslot[V]{child: child}
	}
	return &pnode[V]{bitmap: n.bitmap, slots: slots}, true
}

func (n *pnode[V]) without(pos int, bit uint32) *pnode[V] {
	if len(n.slots) == 1 {
		return nil
	}
	slots := make([]pslot[V], 0, len(n.slots)-1)
	slots = append(slots, n.slots[:pos]...)
	slots = append(slots, n.slots[pos+1:]...)
	return &pnode[V]{bitmap: n.bitmap &^ bit, slots: slots}
}

func (m pmap[V]) each(f func(key string, value V)) {
	m.root.each(f)
}

func (n *pnode[V]) each(f func(key string, value V)) {
	if n == nil {
		return
	}
	for _, e := range n.overflow {
		f(e.key, e.value)
	}
	for _, s := range n.slots {
		if s.child != nil {
			s.child.each(f)
			continue
		}
		f(s.entry.key, s.entry.value)
	}
}

func (m pmap[V]) diff(o pmap[V], f func(key string)) {
	diffNodes(m.root, o.root, f)
}

func diffNodes[V comparable](a, b *pnode[V], f func(key string)) {
	switch {
	case a == b:
		return
	case a == nil:
		b.each(func(key string, _ V) { f(key) })
		return
	case b == nil:
		a.each(func(key string, _ V) { f(key) })
		return
	case len(a.overflow) > 0 || len(b.overflow) > 0:
		diffEntries(a, b, f)
		return
	}
	for union := a.bitmap | b.bitmap; union != 0; union &= union - 1 {
		bit := union & -union
		inA, inB := a.bitmap&bit != 0, b.bitmap&bit != 0
		switch {
		case !inA:
			b.slotNode(bit).each(func(key string, _ V) { f(key) })
		case !inB:
			a.slotNode(bit).each(func(key string, _ V) { f(key) })
		default:
			sa, sb := a.slots[a.position(bit)], b.slots[b.position(bit)]
			if sa.child != nil && sb.child != nil {
				diffNodes(sa.child, sb.child, f)
				continue
			}
			diffEntries(a.slotNode(bit), b.slotNode(bit), f)
		}
	}
}

func (n *pnode[V]) slotNode(bit uint32) *pnode[V] {
	slot := n.slots[n.position(bit)]
	if slot.child != nil {
		return slot.child
	}
	return &pnode[V]{overflow: []pentry[V]{slot.entry}}
}

func diffEntries[V comparable](a, b *pnode[V], f func(key string)) {
	inB := map[string]V{}
	b.each(func(key string, value V) { inB[key] = value })
	a.each(func(key string, value V) {
		other, ok := inB[key]
		if !ok || other != value {
			f(key)
		}
		delete(inB, key)
	})
	for key := range inB {
		f(key)
	}
}
