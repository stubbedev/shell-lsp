package builtins

import (
	"cmp"
	"slices"
	"sync"
)

const HiddenAliasPrefix = "__"

type Builtin struct {
	Name   string
	Params []string
	Doc    string
	Global bool
}

var table = sync.OnceValue(func() []Builtin {
	entries := make([]Builtin, 0, 2*len(members)+1)
	entries = append(entries, parserGlobal)
	for _, m := range members {
		doc := m.fullDoc()
		entries = append(entries, Builtin{Name: m.name, Params: m.params, Doc: doc, Global: m.global})
		if m.global {
			continue
		}
		entries = append(entries, Builtin{
			Name:   HiddenAliasPrefix + m.name,
			Params: m.params,
			Doc:    "Alias of `builtins." + m.name + "`. " + doc,
			Global: true,
		})
	}
	slices.SortFunc(entries, func(a, b Builtin) int { return cmp.Compare(a.Name, b.Name) })
	return entries
})

func Find(name string) (*Builtin, bool) {
	entries := table()
	i, found := slices.BinarySearchFunc(entries, name, func(b Builtin, target string) int {
		return cmp.Compare(b.Name, target)
	})
	if !found {
		return nil, false
	}
	return &entries[i], true
}

func Lookup(name string) (Builtin, bool) {
	b, found := Find(name)
	if !found {
		return Builtin{}, false
	}
	return *b, true
}

func All() []Builtin {
	return slices.Clone(table())
}
