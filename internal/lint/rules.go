package lint

import (
	"fmt"
	"maps"
)

type family struct {
	codes map[Code]meta
	rules []rule
}

var families = []family{
	foreignFamily,
	quotingFamily,
	portableSyntaxFamily,
	portableExpansionsFamily,
	bracesFamily,
	portableRedirectionsFamily,
	portableTestsFamily,
	portableBuiltinsFamily,
}

var catalog, rules = assemble(families)

func assemble(fs []family) (map[Code]meta, []rule) {
	codes := map[Code]meta{}
	var all []rule
	for _, f := range fs {
		for code := range f.codes {
			if _, dup := codes[code]; dup {
				panic(fmt.Sprintf("lint: %s is registered by two families", code))
			}
		}
		maps.Copy(codes, f.codes)
		all = append(all, f.rules...)
	}
	return codes, all
}
