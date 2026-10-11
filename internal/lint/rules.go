package lint

import (
	"fmt"
	"maps"

	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
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
	fileOpsFamily,
	elevationFamily,
	commandWordFamily,
	variablesFamily,
	testsFamily,
	redirectsFamily,
	expansionsFamily,
	errexitFamily,
	structureFamily,
	flowFamily,
	testOperandsFamily,
	argumentsFamily,
	assignmentsFamily,
	constructsFamily,
	caseFamily,
	controlFamily,
	shebangFamily,
	comparisonsFamily,
	loopsFamily,
	pipelinesFamily,
	redirectSafetyFamily,
	assignmentMistakesFamily,
	arraysFamily,
	singleQuotesFamily,
	arithmeticFamily,
	testLogicFamily,
	arithmeticStyleFamily,
	parameterSyntaxFamily,
	subshellFamily,
	wordsFamily,
	executionFamily,
	taintFamily,
	directivesFamily,
	sourcesFamily,
	typosFamily,
	testSyntaxFamily,
	injectionFamily,
	downloadFamily,
	subshellScopeFamily,
	unusedFunctionsFamily,
	embeddedFamily,
}

type nodeDispatch [][]func(p *pass, n syntax.Node)

func nodes(prototypes ...syntax.Node) []nodeKind {
	out := make([]nodeKind, len(prototypes))
	for i, prototype := range prototypes {
		out[i] = nodeKindOf(prototype)
		if out[i] == kindUnknown {
			panic(fmt.Sprintf("lint: %T is not a dispatchable syntax node", prototype))
		}
	}
	return out
}

var catalog, rules, nodeChecks, commandChecks = assemble(families)

func assemble(fs []family) (map[Code]meta, []rule, nodeDispatch, commandDispatch) {
	codes := map[Code]meta{}
	var all []rule
	dispatch := make(nodeDispatch, nodeKinds)
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
	for _, r := range all {
		if r.node == nil {
			continue
		}
		if len(r.on) == 0 {
			panic("lint: a node rule must declare the node types it handles")
		}
		for _, t := range r.on {
			dispatch[t] = append(dispatch[t], r.node)
		}
	}
	return codes, all, dispatch, newCommandDispatch(commands)
}
