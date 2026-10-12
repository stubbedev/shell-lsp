package lint

import (
	"fmt"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type family struct {
	codes    map[Code]meta
	nodes    []nodeRule
	finish   []func(p *pass)
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
	localVariablesFamily,
	globalReassignmentFamily,
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
	testGroupingFamily,
	arithmeticStyleFamily,
	parameterSyntaxFamily,
	subshellFamily,
	wordsFamily,
	executionFamily,
	taintFamily,
	directivesFamily,
	unusedDirectivesFamily,
	sourcesFamily,
	relativeSourceFamily,
	typosFamily,
	testSyntaxFamily,
	injectionFamily,
	downloadFamily,
	permissionsFamily,
	subshellScopeFamily,
	tempFileFamily,
	unusedFunctionsFamily,
	embeddedFamily,
	googleFamily,
}

type nodeDispatch [][]func(p *pass, n syntax.Node)

type nodeRule struct {
	on    []nodeKind
	check func(p *pass, n syntax.Node)
}

func onNode[T syntax.Node](check func(p *pass, n T)) nodeRule {
	return nodeRule{on: nodes(*new(T)), check: func(p *pass, n syntax.Node) {
		typed, ok := n.(T)
		if !ok {
			panic(fmt.Sprintf("lint: a %T node was dispatched to a rule for %T", n, *new(T)))
		}
		check(p, typed)
	}}
}

func onNodes(check func(p *pass, n syntax.Node), first syntax.Node, rest ...syntax.Node) nodeRule {
	return nodeRule{on: nodes(append([]syntax.Node{first}, rest...)...), check: check}
}

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

var (
	catalog       map[Code]meta
	finishers     []func(p *pass)
	nodeChecks    nodeDispatch
	commandChecks commandDispatch
)

func init() {
	catalog, finishers, nodeChecks, commandChecks = assemble(families)
}

func registered(code Code, m meta) meta {
	switch {
	case code.Portability() != (m.shells != 0):
		panic(fmt.Sprintf("lint: %s must name the shells it is reported in exactly when it is a portability code", code))
	case m.shells == 0:
		return m
	case m.severity != 0:
		panic(fmt.Sprintf("lint: %s takes its severity from its shells", code))
	case m.shells.Has(dialect.Posix):
		m.severity = SeverityWarning
	default:
		m.severity = SeverityError
	}
	return m
}

func assemble(fs []family) (map[Code]meta, []func(p *pass), nodeDispatch, commandDispatch) {
	codes := map[Code]meta{}
	var finish []func(p *pass)
	dispatch := make(nodeDispatch, nodeKinds)
	var commands []commandRule
	for _, f := range fs {
		for code := range f.codes {
			if _, dup := codes[code]; dup {
				panic(fmt.Sprintf("lint: %s is registered by two families", code))
			}
		}
		for code, m := range f.codes {
			codes[code] = registered(code, m)
		}
		for _, r := range f.nodes {
			for _, kind := range r.on {
				dispatch[kind] = append(dispatch[kind], r.check)
			}
		}
		finish = append(finish, f.finish...)
		commands = append(commands, f.commands...)
	}
	return codes, finish, dispatch, newCommandDispatch(commands)
}
