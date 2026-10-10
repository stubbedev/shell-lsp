package lint

import (
	"maps"
	"math/rand/v2"
	"slices"
	"strconv"
	"testing"
)

func contents(m pmap[int]) map[string]int {
	out := map[string]int{}
	m.each(func(key string, value int) { out[key] = value })
	return out
}

func TestPmapMatchesMap(t *testing.T) {
	t.Parallel()

	for seed := range 50 {
		rng := rand.New(rand.NewPCG(uint64(seed), 99))
		var m pmap[int]
		model := map[string]int{}
		snapshots := []struct {
			m     pmap[int]
			model map[string]int
		}{}
		for range 2000 {
			key := "v" + strconv.Itoa(rng.IntN(300))
			switch rng.IntN(4) {
			case 0:
				m = m.remove(key)
				delete(model, key)
			default:
				value := rng.IntN(5)
				m = m.set(key, value)
				model[key] = value
			}
			if rng.IntN(50) == 0 {
				snapshots = append(snapshots, struct {
					m     pmap[int]
					model map[string]int
				}{m, maps.Clone(model)})
			}
		}
		if got := contents(m); !maps.Equal(got, model) {
			t.Fatalf("seed %d: contents = %v, want %v", seed, got, model)
		}
		for key, want := range model {
			if got, ok := m.get(key); !ok || got != want {
				t.Fatalf("seed %d: get(%s) = %d, %t; want %d", seed, key, got, ok, want)
			}
		}
		for _, snap := range snapshots {
			if got := contents(snap.m); !maps.Equal(got, snap.model) {
				t.Fatalf("seed %d: an older version changed after later writes", seed)
			}
			var differing []string
			snap.m.diff(m, func(key string) { differing = append(differing, key) })
			var want []string
			for key := range keysOf(snap.model, model) {
				if a, okA := snap.model[key]; !okA || model[key] != a || !hasKey(model, key) {
					want = append(want, key)
				}
			}
			slices.Sort(differing)
			slices.Sort(want)
			if !slices.Equal(slices.Compact(differing), want) {
				t.Fatalf("seed %d: diff = %v, want %v", seed, differing, want)
			}
		}
	}
}

func keysOf(a, b map[string]int) map[string]bool {
	out := map[string]bool{}
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}

func hasKey(m map[string]int, key string) bool {
	_, ok := m[key]
	return ok
}

func TestPmapSharesUnchangedWrites(t *testing.T) {
	t.Parallel()

	m := pmap[int]{}.set("a", 1).set("b", 2)
	if same := m.set("a", 1); same.root != m.root {
		t.Error("rewriting an identical value copied the trie")
	}
	if same := m.remove("missing"); same.root != m.root {
		t.Error("removing a missing key copied the trie")
	}
	var calls int
	m.diff(m.set("b", 2), func(string) { calls++ })
	if calls != 0 {
		t.Errorf("diff of equal maps reported %d keys", calls)
	}
}

func TestPmapFullHashCollisions(t *testing.T) {
	t.Parallel()

	const h, depth = 0x5eed, trieMaxDepth - 1
	var n *pnode[int]
	for i, key := range []string{"a", "b", "c"} {
		n = insert(n, pentry[int]{key: key, value: i}, h, depth)
	}
	before := n
	n = insert(n, pentry[int]{key: "b", value: 9}, h, depth)
	got := map[string]int{}
	n.each(func(key string, value int) { got[key] = value })
	if want := map[string]int{"a": 0, "b": 9, "c": 2}; !maps.Equal(got, want) {
		t.Fatalf("colliding contents = %v, want %v", got, want)
	}
	var changed []string
	diffNodes(before, n, depth, func(key string) { changed = append(changed, key) })
	if !slices.Equal(changed, []string{"b"}) {
		t.Errorf("diff across colliding keys = %v, want [b]", changed)
	}
	if same := insert(n, pentry[int]{key: "a", value: 0}, h, depth); same != n {
		t.Error("rewriting an identical colliding value copied the trie")
	}
	for _, key := range []string{"a", "c"} {
		n = erase(n, h, key, depth)
	}
	if n == nil || len(n.children) != 0 || len(n.entries) != 1 || n.entries[0] != (pentry[int]{key: "b", value: 9}) {
		t.Fatalf("after erasing all but one colliding key the root = %+v, want the survivor inlined", n)
	}
	if n = erase(n, h, "b", depth); n != nil {
		t.Errorf("erasing the last key left %+v", n)
	}
}
