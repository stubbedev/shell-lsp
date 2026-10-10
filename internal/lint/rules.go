package lint

import (
	"fmt"
	"maps"
)

type family struct {
	codes    map[Code]meta
	rules    []rule
	commands []commandRule
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
	sedSubstitutionFamily,
	negationFamily,
	bracketsFamily,
	escapesFamily,
	charactersFamily,
	heredocsFamily,
	misreadsFamily,
	outputFamily,
	trFamily,
	grepFamily,
	findFamily,
	exprFamily,
	declarationFamily,
	builtinFamily,
	keywordFamily,
	getoptsFamily,
}

var catalog, rules, commandChecks = assemble(families)

func assemble(fs []family) (map[Code]meta, []rule, commandDispatch) {
	codes := map[Code]meta{}
	var all []rule
	var commands []commandRule
	for _, f := range fs {
		for code := range f.codes {
			if _, dup := codes[code]; dup {
				panic(fmt.Sprintf("lint: %s is registered by two families", code))
			}
		}
		maps.Copy(codes, f.codes)
		all = append(all, f.rules...)
		commands = append(commands, f.commands...)
	}
	return codes, all, newCommandDispatch(commands)
}
