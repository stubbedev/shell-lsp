package nix

import (
	"slices"
)

const linearNames = 8

type nameTable[V any] struct {
	keys  []string
	vals  []V
	index map[string]int32
}

func (t *nameTable[V]) find(name string) int {
	if t.index != nil {
		if i, ok := t.index[name]; ok {
			return int(i)
		}
		return -1
	}
	return slices.Index(t.keys, name)
}

func (t *nameTable[V]) get(name string) (V, bool) {
	if i := t.find(name); i >= 0 {
		return t.vals[i], true
	}
	var zero V
	return zero, false
}

func (t *nameTable[V]) of(name string) V {
	v, _ := t.get(name)
	return v
}

func (t *nameTable[V]) has(name string) bool { return t.find(name) >= 0 }

func (t *nameTable[V]) set(name string, v V) {
	if i := t.find(name); i >= 0 {
		t.vals[i] = v
		return
	}
	t.keys = append(t.keys, name)
	t.vals = append(t.vals, v)
	switch {
	case t.index != nil:
		t.index[name] = int32(len(t.keys) - 1) //nolint:gosec // bounded by the source length, which the transport caps well below 2^31
	case len(t.keys) > linearNames:
		t.reindex()
	}
}

func (t *nameTable[V]) reindex() {
	t.index = make(map[string]int32, 2*len(t.keys))
	for i, k := range t.keys {
		t.index[k] = int32(i)
	}
}

func (t *nameTable[V]) reserve(n int) {
	if n <= linearNames {
		return
	}
	t.keys = slices.Grow(t.keys, n)
	t.vals = slices.Grow(t.vals, n)
}

func (t *nameTable[V]) sorted() []string {
	return slices.Sorted(slices.Values(t.keys))
}
