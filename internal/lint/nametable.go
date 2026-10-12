package lint

import "fmt"

type nameTable[E ~uint8] []string

func (t nameTable[E]) name(e E) string {
	if int(e) >= len(t) || t[e] == "" {
		panic(fmt.Sprintf("lint: %T %d has no name", e, e))
	}
	return t[e]
}

func (t nameTable[E]) named(name string) (E, bool) {
	for e, n := range t {
		if n != "" && n == name {
			return E(e), true
		}
	}
	return 0, false
}
