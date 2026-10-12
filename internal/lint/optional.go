package lint

import "github.com/stubbedev/shell-lsp/internal/directive"

type optionalCheck uint8

const (
	notOptional optionalCheck = iota
	optionalDefaultCase
	optionalArithmeticInjection
	optionalGlobalReassignment
	optionalNegatedConditions
	optionalNullaryConditions
	optionalMaskedReturns
	optionalRelativeSources
	optionalSetESuppressed
	optionalUnassignedUppercase
	optionalDeprecateWhich
	optionalGoogleFileHeader
	optionalGoogleShebang
	optionalGoogleTodo
	optionalQuoteSafeVariables
	optionalDoubleBrackets
	optionalDoubleEquals
	optionalLocalVariables
	optionalTestGrouping
	optionalVariableBraces
	optionalUnusedDirectives
	optionalUselessCat
	optionalCheckCount
)

type optionalSpec struct {
	name      string
	changes   string
	notStrict bool
}

var optionalSpecs = [optionalCheckCount]optionalSpec{
	optionalDefaultCase:         {name: "add-default-case"},
	optionalArithmeticInjection: {name: "arithmetic-injection"},
	optionalGlobalReassignment:  {name: "avoid-global-reassignment", notStrict: true},
	optionalNegatedConditions:   {name: "avoid-negated-conditions"},
	optionalNullaryConditions:   {name: "avoid-nullary-conditions"},
	optionalMaskedReturns:       {name: "check-extra-masked-returns"},
	optionalRelativeSources:     {name: "check-relative-sources"},
	optionalSetESuppressed:      {name: "check-set-e-suppressed"},
	optionalUnassignedUppercase: {
		name:      "check-unassigned-uppercase",
		changes:   "SC2154 also reports unassigned variables with all-uppercase names, which are otherwise taken to come from the environment.",
		notStrict: true,
	},
	optionalDeprecateWhich:     {name: "deprecate-which"},
	optionalGoogleFileHeader:   {name: "google-file-header", notStrict: true},
	optionalGoogleShebang:      {name: "google-shebang", notStrict: true},
	optionalGoogleTodo:         {name: "google-todo", notStrict: true},
	optionalQuoteSafeVariables: {name: "quote-safe-variables"},
	optionalDoubleBrackets:     {name: "require-double-brackets"},
	optionalDoubleEquals:       {name: "require-double-equals"},
	optionalLocalVariables:     {name: "require-local-variables"},
	optionalTestGrouping:       {name: "require-test-grouping"},
	optionalVariableBraces:     {name: "require-variable-braces"},
	optionalUnusedDirectives:   {name: "unused-directives"},
	optionalUselessCat:         {name: "useless-use-of-cat"},
}

func (c optionalCheck) String() string { return optionalSpecs[c].name }

func optionalNamed(name string) (optionalCheck, bool) {
	for c := notOptional + 1; c < optionalCheckCount; c++ {
		if optionalSpecs[c].name == name {
			return c, true
		}
	}
	return notOptional, false
}

type optionalSet uint32

const allOptional optionalSet = 1<<optionalCheckCount - 1

func (s optionalSet) has(c optionalCheck) bool { return s&(1<<c) != 0 }

func (s *optionalSet) enable(name string) {
	if name == directive.All {
		*s = allOptional
		return
	}
	if c, ok := optionalNamed(name); ok {
		*s |= 1 << c
	}
}

type OptionalCheck struct {
	Name    string
	Summary string
	Codes   []Code
}

func OptionalChecks() []OptionalCheck {
	out := make([]OptionalCheck, 0, optionalCheckCount-1)
	for c := notOptional + 1; c < optionalCheckCount; c++ {
		check := OptionalCheck{Name: c.String(), Summary: optionalSpecs[c].changes}
		for _, code := range Codes() {
			if catalog[code].optional == c {
				check.Codes = append(check.Codes, code)
			}
		}
		out = append(out, check)
	}
	return out
}

func EnablesOptional(name string) bool {
	_, ok := optionalNamed(name)
	return ok || name == directive.All
}
